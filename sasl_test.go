package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// saslTestHelper connects to a SASL server, performs the handshake, and returns
// a reader/writer pair ready for AUTH requests.
type saslTestHelper struct {
	t      *testing.T
	conn   net.Conn
	reader *bufio.Reader
	writer *bufio.Writer
}

func newSASLTestHelper(t *testing.T, addr string) *saslTestHelper {
	t.Helper()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)

	h := &saslTestHelper{
		t:      t,
		conn:   conn,
		reader: bufio.NewReader(conn),
		writer: bufio.NewWriter(conn),
	}

	return h
}

func (h *saslTestHelper) close() {
	h.conn.Close()
}

// readLine reads a single \n-terminated line.
func (h *saslTestHelper) readLine() string {
	h.t.Helper()
	h.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	line, err := h.reader.ReadString('\n')
	require.NoError(h.t, err)
	return strings.TrimRight(line, "\r\n")
}

// writeLine writes a line followed by \n and flushes.
func (h *saslTestHelper) writeLine(line string) {
	h.t.Helper()
	_, err := h.writer.WriteString(line + "\n")
	require.NoError(h.t, err)
	require.NoError(h.t, h.writer.Flush())
}

// readServerHandshake reads the full server handshake and returns the lines.
func (h *saslTestHelper) readServerHandshake() []string {
	h.t.Helper()
	var lines []string
	for {
		line := h.readLine()
		lines = append(lines, line)
		if line == "DONE" {
			break
		}
	}
	return lines
}

// sendClientHandshake sends the standard Postfix client handshake.
func (h *saslTestHelper) sendClientHandshake() {
	h.t.Helper()
	h.writeLine("VERSION\t1\t0")
	h.writeLine("CPID\t12345")
}

// doHandshake reads server handshake, sends client handshake.
func (h *saslTestHelper) doHandshake() []string {
	h.t.Helper()
	lines := h.readServerHandshake()
	h.sendClientHandshake()
	return lines
}

// sendPlainAuth sends a PLAIN AUTH request and returns the response line.
func (h *saslTestHelper) sendPlainAuth(id, email, password string) string {
	h.t.Helper()
	// RFC 4616: \x00authcid\x00passwd
	payload := "\x00" + email + "\x00" + password
	resp := base64.StdEncoding.EncodeToString([]byte(payload))
	h.writeLine(fmt.Sprintf("AUTH\t%s\tPLAIN\tservice=smtp\tnologin\tresp=%s", id, resp))
	return h.readLine()
}

// sendLoginAuth performs the LOGIN mechanism exchange and returns the final response.
func (h *saslTestHelper) sendLoginAuth(id, email, password string) string {
	h.t.Helper()
	h.writeLine(fmt.Sprintf("AUTH\t%s\tLOGIN\tservice=smtp\tnologin", id))

	// Read Username challenge
	challenge := h.readLine()
	require.True(h.t, strings.HasPrefix(challenge, "CONT\t"+id+"\t"), "expected CONT for username challenge")

	// Send username
	h.writeLine(fmt.Sprintf("CONT\t%s\t%s", id, base64.StdEncoding.EncodeToString([]byte(email))))

	// Read Password challenge
	challenge = h.readLine()
	require.True(h.t, strings.HasPrefix(challenge, "CONT\t"+id+"\t"), "expected CONT for password challenge")

	// Send password
	h.writeLine(fmt.Sprintf("CONT\t%s\t%s", id, base64.StdEncoding.EncodeToString([]byte(password))))

	return h.readLine()
}

// startSASLTestServer starts a SASL server on a random port and returns the address.
// The caller must cancel ctx to stop the server.
func startSASLTestServer(t *testing.T, mockService *MockUserliService) (string, context.CancelFunc) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := listener.Addr().String()
	listener.Close()

	server := NewSASLServer(mockService, zap.NewNop())
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	wg.Add(1)
	go StartSASLServer(ctx, &wg, addr, server)

	// Wait for server to be ready
	time.Sleep(100 * time.Millisecond)

	t.Cleanup(func() {
		cancel()
		wg.Wait()
	})

	return addr, cancel
}

func TestSASL_Handshake(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()

	lines := h.readServerHandshake()

	// Verify handshake lines. MECH must come before SPID so Postfix
	// recognizes this as an auth-client (not auth-master) socket.
	assert.True(t, strings.HasPrefix(lines[0], "VERSION\t1\t2"), "first line should be VERSION")
	assert.Equal(t, "MECH\tPLAIN\tplaintext", lines[1])
	assert.Equal(t, "MECH\tLOGIN\tplaintext", lines[2])
	assert.True(t, strings.HasPrefix(lines[3], "SPID\t"), "fourth line should be SPID")
	assert.True(t, strings.HasPrefix(lines[4], "CUID\t"), "fifth line should be CUID")
	assert.True(t, strings.HasPrefix(lines[5], "COOKIE\t"), "sixth line should be COOKIE")

	// Check COOKIE is 32 hex chars
	cookie := strings.TrimPrefix(lines[5], "COOKIE\t")
	assert.Len(t, cookie, 32)

	assert.Equal(t, "DONE", lines[6])

	// Send client handshake
	h.sendClientHandshake()
}

func TestSASL_Handshake_VersionMismatch(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()

	h.readServerHandshake()

	// Send incompatible major version
	h.writeLine("VERSION\t2\t0")
	h.writeLine("CPID\t12345")

	// Connection should be closed by server due to version mismatch
	h.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err := h.reader.ReadString('\n')
	assert.Error(t, err, "server should close connection on version mismatch")
}

func TestSASL_PlainAuth_Success(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	mockService.On("Authenticate", mock.Anything, "user@example.org", "secret").Return(true, "success", nil)
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	resp := h.sendPlainAuth("1", "user@example.org", "secret")
	assert.Equal(t, "OK\t1\tuser=user@example.org", resp)
	mockService.AssertExpectations(t)
}

func TestSASL_PlainAuth_InvalidCredentials(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	mockService.On("Authenticate", mock.Anything, "user@example.org", "wrong").Return(false, "authentication failed", nil)
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	resp := h.sendPlainAuth("1", "user@example.org", "wrong")
	assert.True(t, strings.HasPrefix(resp, "FAIL\t1\t"), "expected FAIL response")
	assert.Contains(t, resp, "user=user@example.org")
	assert.Contains(t, resp, "reason=authentication failed")
	mockService.AssertExpectations(t)
}

func TestSASL_PlainAuth_APIError(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	mockService.On("Authenticate", mock.Anything, "user@example.org", "secret").Return(false, "", fmt.Errorf("connection refused"))
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	// Fail-closed: API errors should reject authentication
	resp := h.sendPlainAuth("1", "user@example.org", "secret")
	assert.True(t, strings.HasPrefix(resp, "FAIL\t1\t"), "expected FAIL on API error")
	mockService.AssertExpectations(t)
}

func TestSASL_PlainAuth_MissingResp(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	// Send PLAIN without resp= parameter
	h.writeLine("AUTH\t1\tPLAIN\tservice=smtp\tnologin")
	resp := h.readLine()
	assert.True(t, strings.HasPrefix(resp, "FAIL\t1\t"))
}

func TestSASL_PlainAuth_InvalidBase64(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	h.writeLine("AUTH\t1\tPLAIN\tservice=smtp\tresp=not-valid-base64!!!")
	resp := h.readLine()
	assert.True(t, strings.HasPrefix(resp, "FAIL\t1\t"))
}

func TestSASL_LoginAuth_Success(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	mockService.On("Authenticate", mock.Anything, "user@example.org", "secret").Return(true, "success", nil)
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	resp := h.sendLoginAuth("1", "user@example.org", "secret")
	assert.Equal(t, "OK\t1\tuser=user@example.org", resp)
	mockService.AssertExpectations(t)
}

func TestSASL_LoginAuth_InvalidCredentials(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	mockService.On("Authenticate", mock.Anything, "user@example.org", "wrong").Return(false, "authentication failed", nil)
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	resp := h.sendLoginAuth("1", "user@example.org", "wrong")
	assert.True(t, strings.HasPrefix(resp, "FAIL\t1\t"))
	mockService.AssertExpectations(t)
}

func TestSASL_UnsupportedMechanism(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	h.writeLine("AUTH\t1\tCRAM-MD5\tservice=smtp")
	resp := h.readLine()
	assert.True(t, strings.HasPrefix(resp, "FAIL\t1\t"))
	assert.Contains(t, resp, "Unsupported mechanism")
}

func TestSASL_MultipleRequests(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	mockService.On("Authenticate", mock.Anything, "user@example.org", "secret").Return(true, "success", nil)
	mockService.On("Authenticate", mock.Anything, "other@example.org", "pass").Return(false, "authentication failed", nil)
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	// First request: success
	resp1 := h.sendPlainAuth("1", "user@example.org", "secret")
	assert.True(t, strings.HasPrefix(resp1, "OK\t1\t"))

	// Second request on same connection: failure
	resp2 := h.sendPlainAuth("2", "other@example.org", "pass")
	assert.True(t, strings.HasPrefix(resp2, "FAIL\t2\t"))

	mockService.AssertExpectations(t)
}

func TestSASL_InvalidAuthRequest(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	// AUTH with too few fields
	h.writeLine("AUTH\t1")
	resp := h.readLine()
	assert.True(t, strings.HasPrefix(resp, "FAIL\t"))
}

func TestSASL_PlainAuth_EmptyCredentials(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	// PLAIN with empty username
	payload := "\x00\x00password"
	resp := base64.StdEncoding.EncodeToString([]byte(payload))
	h.writeLine(fmt.Sprintf("AUTH\t1\tPLAIN\tresp=%s", resp))
	line := h.readLine()
	assert.True(t, strings.HasPrefix(line, "FAIL\t1\t"))
}

func TestSASL_CUIDIncrementsPerConnection(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	addr, _ := startSASLTestServer(t, mockService)

	// First connection
	h1 := newSASLTestHelper(t, addr)
	lines1 := h1.readServerHandshake()
	h1.close()

	// Second connection
	h2 := newSASLTestHelper(t, addr)
	lines2 := h2.readServerHandshake()
	h2.close()

	// Extract CUID values
	var cuid1, cuid2 string
	for _, l := range lines1 {
		if strings.HasPrefix(l, "CUID\t") {
			cuid1 = strings.TrimPrefix(l, "CUID\t")
		}
	}
	for _, l := range lines2 {
		if strings.HasPrefix(l, "CUID\t") {
			cuid2 = strings.TrimPrefix(l, "CUID\t")
		}
	}

	assert.NotEmpty(t, cuid1)
	assert.NotEmpty(t, cuid2)
	assert.NotEqual(t, cuid1, cuid2, "CUID should increment per connection")
}

// assertNoTempParameter asserts that a FAIL response does not contain the
// `temp` flag or `temp=` parameter — Postfix maps either to 454 (retryable),
// while permanent auth failures must produce 535.
func assertNoTempParameter(t *testing.T, resp string) {
	t.Helper()
	require.True(t, strings.HasPrefix(resp, "FAIL\t"), "expected FAIL response, got: %q", resp)
	fields := strings.Split(resp, "\t")
	for _, f := range fields[2:] {
		assert.NotEqual(t, "temp", f, "FAIL must not contain bare temp parameter")
		assert.False(t, strings.HasPrefix(f, "temp="), "FAIL must not contain temp= parameter, got field: %q", f)
	}
}

func TestSASL_FailResponse_NoTempParameter(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	mockService.On("Authenticate", mock.Anything, "user@example.org", "wrong").
		Return(false, "authentication failed", nil)
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	resp := h.sendPlainAuth("1", "user@example.org", "wrong")
	assertNoTempParameter(t, resp)
	mockService.AssertExpectations(t)
}

func TestSASL_FailResponse_APIError_NoTempParameter(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	mockService.On("Authenticate", mock.Anything, "user@example.org", "secret").
		Return(false, "", fmt.Errorf("connection refused"))
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	resp := h.sendPlainAuth("1", "user@example.org", "secret")
	assertNoTempParameter(t, resp)
	mockService.AssertExpectations(t)
}

func TestSASL_FailResponse_SanitizesReason(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	mockService.On("Authenticate", mock.Anything, "user@example.org", "wrong").
		Return(false, "bad\tcreds\nfor user", nil)
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	resp := h.sendPlainAuth("1", "user@example.org", "wrong")
	require.True(t, strings.HasPrefix(resp, "FAIL\t1\t"))

	var reasonField string
	for f := range strings.SplitSeq(resp, "\t") {
		if rest, ok := strings.CutPrefix(f, "reason="); ok {
			reasonField = rest
		}
	}
	require.NotEmpty(t, reasonField)
	assert.NotContains(t, reasonField, "\t")
	assert.NotContains(t, reasonField, "\n")
	assert.NotContains(t, reasonField, "\r")
	assert.Equal(t, "bad creds for user", reasonField)
	mockService.AssertExpectations(t)
}

// TestSASL_IdleConnection_ClosesCleanly verifies that the server cleanly
// closes idle connections via the per-iteration ReadDeadline rather than
// holding onto them under the global ConnectionTimeout (which previously
// caused `reader.ReadString` to time out mid-AUTH after 60s, closing the
// connection without sending a FAIL — Postfix then mapped that EOF to
// 454 "Connection lost to authentication server" instead of 535).
func TestSASL_IdleConnection_ClosesCleanly(t *testing.T) {
	logger = zap.NewNop()
	mockService := &MockUserliService{}
	addr, _ := startSASLTestServer(t, mockService)

	h := newSASLTestHelper(t, addr)
	defer h.close()
	h.doHandshake()

	// Allow more than ReadTimeout (10s) of idle so the server closes us.
	h.conn.SetReadDeadline(time.Now().Add(ReadTimeout + 5*time.Second))
	_, err := h.reader.ReadString('\n')
	assert.ErrorIs(t, err, io.EOF, "server must close idle connections cleanly")
}
