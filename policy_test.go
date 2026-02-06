package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// MockUserliService is a mock implementation for testing
type MockUserliServiceForPolicy struct {
	quota    *Quota
	quotaErr error
}

func (m *MockUserliServiceForPolicy) GetAliases(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (m *MockUserliServiceForPolicy) GetDomain(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (m *MockUserliServiceForPolicy) GetMailbox(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (m *MockUserliServiceForPolicy) GetSenders(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (m *MockUserliServiceForPolicy) GetQuota(_ context.Context, _ string) (*Quota, error) {
	if m.quotaErr != nil {
		return nil, m.quotaErr
	}
	return m.quota, nil
}

func TestPolicyServer_ReadRequest(t *testing.T) {
	input := `request=smtpd_access_policy
protocol_state=END-OF-MESSAGE
protocol_name=SMTP
sender=user@example.org
recipient=recipient@example.com
sasl_username=user@example.org
client_address=192.168.1.1

`
	reader := bufio.NewReader(strings.NewReader(input))
	server := &PolicyServer{}

	req, err := server.readRequest(reader)
	if err != nil {
		t.Fatalf("Failed to read request: %v", err)
	}

	if req.Request != "smtpd_access_policy" {
		t.Errorf("Expected request=smtpd_access_policy, got %s", req.Request)
	}
	if req.ProtocolState != "END-OF-MESSAGE" {
		t.Errorf("Expected protocol_state=END-OF-MESSAGE, got %s", req.ProtocolState)
	}
	if req.Sender != "user@example.org" {
		t.Errorf("Expected sender=user@example.org, got %s", req.Sender)
	}
	if req.SaslUsername != "user@example.org" {
		t.Errorf("Expected sasl_username=user@example.org, got %s", req.SaslUsername)
	}
}

func TestPolicyServer_HandleRequest_SkipNonEndOfMessage(t *testing.T) {
	mockClient := &MockUserliServiceForPolicy{
		quota: &Quota{PerHour: 10, PerDay: 100},
	}
	rateLimiter := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}
	server := NewPolicyServer(mockClient, rateLimiter)

	req := &PolicyRequest{
		ProtocolState: "RCPT",
		Sender:        "user@example.org",
		SaslUsername:  "user@example.org",
	}

	response := server.handleRequest(context.Background(), req)

	if response != "DUNNO" {
		t.Errorf("Expected DUNNO for non-END-OF-MESSAGE state, got %s", response)
	}
}

func TestPolicyServer_HandleRequest_NoSender(t *testing.T) {
	mockClient := &MockUserliServiceForPolicy{
		quota: &Quota{PerHour: 10, PerDay: 100},
	}
	rateLimiter := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}
	server := NewPolicyServer(mockClient, rateLimiter)

	req := &PolicyRequest{
		ProtocolState: "END-OF-MESSAGE",
		Sender:        "",
		SaslUsername:  "",
	}

	response := server.handleRequest(context.Background(), req)

	if response != "DUNNO" {
		t.Errorf("Expected DUNNO for empty sender, got %s", response)
	}
}

func TestPolicyServer_HandleRequest_APIError(t *testing.T) {
	mockClient := &MockUserliServiceForPolicy{
		quotaErr: fmt.Errorf("API error"),
	}
	rateLimiter := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}
	server := NewPolicyServer(mockClient, rateLimiter)

	req := &PolicyRequest{
		ProtocolState: "END-OF-MESSAGE",
		Sender:        "user@example.org",
		SaslUsername:  "user@example.org",
	}

	response := server.handleRequest(context.Background(), req)

	// Should fail open (allow message) when API is unavailable
	if response != "DUNNO" {
		t.Errorf("Expected DUNNO on API error (fail open), got %s", response)
	}
}

func TestPolicyServer_HandleRequest_NoLimits(t *testing.T) {
	mockClient := &MockUserliServiceForPolicy{
		quota: &Quota{PerHour: 0, PerDay: 0},
	}
	rateLimiter := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}
	server := NewPolicyServer(mockClient, rateLimiter)

	req := &PolicyRequest{
		ProtocolState: "END-OF-MESSAGE",
		Sender:        "user@example.org",
		SaslUsername:  "user@example.org",
	}

	response := server.handleRequest(context.Background(), req)

	if response != "DUNNO" {
		t.Errorf("Expected DUNNO for unlimited quota, got %s", response)
	}
}

func TestPolicyServer_HandleRequest_AllowedMessage(t *testing.T) {
	mockClient := &MockUserliServiceForPolicy{
		quota: &Quota{PerHour: 10, PerDay: 100},
	}
	rateLimiter := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}
	server := NewPolicyServer(mockClient, rateLimiter)

	req := &PolicyRequest{
		ProtocolState: "END-OF-MESSAGE",
		Sender:        "user@example.org",
		SaslUsername:  "user@example.org",
	}

	response := server.handleRequest(context.Background(), req)

	if response != "DUNNO" {
		t.Errorf("Expected DUNNO for allowed message, got %s", response)
	}
}

func TestPolicyServer_HandleRequest_RateLimited(t *testing.T) {
	mockClient := &MockUserliServiceForPolicy{
		quota: &Quota{PerHour: 2, PerDay: 100},
	}
	rateLimiter := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}
	server := NewPolicyServer(mockClient, rateLimiter)

	req := &PolicyRequest{
		ProtocolState: "END-OF-MESSAGE",
		Sender:        "user@example.org",
		SaslUsername:  "user@example.org",
	}

	// First 2 messages should pass
	for i := 0; i < 2; i++ {
		response := server.handleRequest(context.Background(), req)
		if response != "DUNNO" {
			t.Errorf("Message %d should be allowed, got %s", i+1, response)
		}
	}

	// 3rd message should be rejected
	response := server.handleRequest(context.Background(), req)
	if !strings.HasPrefix(response, "REJECT") {
		t.Errorf("3rd message should be rejected, got %s", response)
	}
	if !strings.Contains(response, "Rate limit exceeded") {
		t.Errorf("Expected rate limit message, got %s", response)
	}
}

func TestPolicyServer_HandleRequest_UsesSaslUsername(t *testing.T) {
	mockClient := &MockUserliServiceForPolicy{
		quota: &Quota{PerHour: 1, PerDay: 100},
	}
	rateLimiter := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}
	server := NewPolicyServer(mockClient, rateLimiter)

	// First request uses sasl_username
	req1 := &PolicyRequest{
		ProtocolState: "END-OF-MESSAGE",
		Sender:        "different@example.org",
		SaslUsername:  "user@example.org",
	}
	server.handleRequest(context.Background(), req1)

	// Second request with same sasl_username should be limited
	req2 := &PolicyRequest{
		ProtocolState: "END-OF-MESSAGE",
		Sender:        "another@example.org",
		SaslUsername:  "user@example.org",
	}
	response := server.handleRequest(context.Background(), req2)

	if !strings.HasPrefix(response, "REJECT") {
		t.Errorf("Should use sasl_username for rate limiting, got %s", response)
	}
}

func TestPolicyServer_Integration(t *testing.T) {
	mockClient := &MockUserliServiceForPolicy{
		quota: &Quota{PerHour: 100, PerDay: 1000},
	}
	rateLimiter := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}
	server := NewPolicyServer(mockClient, rateLimiter)

	// Start server on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().String()

	// Handle one connection in background
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		reader := bufio.NewReader(conn)
		req, _ := server.readRequest(reader)
		response := server.handleRequest(context.Background(), req)
		_ = server.writeResponse(conn, response)
		conn.Close()
	}()

	// Connect as client
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer conn.Close()

	// Send policy request
	request := `request=smtpd_access_policy
protocol_state=END-OF-MESSAGE
sender=test@example.org
sasl_username=test@example.org

`
	_, err = conn.Write([]byte(request))
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	// Read response
	reader := bufio.NewReader(conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "action=DUNNO") {
		t.Errorf("Expected action=DUNNO, got %s", response)
	}
}

func TestPolicyServer_HandleConnection(t *testing.T) {
	mockClient := &MockUserliServiceForPolicy{
		quota: &Quota{PerHour: 100, PerDay: 1000},
	}
	rateLimiter := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}
	server := NewPolicyServer(mockClient, rateLimiter)

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	// Start HandleConnection in background
	done := make(chan struct{})
	go func() {
		server.HandleConnection(context.Background(), serverConn)
		close(done)
	}()

	// Send policy request
	request := "request=smtpd_access_policy\nprotocol_state=END-OF-MESSAGE\nsender=test@example.org\n\n"
	_, err := clientConn.Write([]byte(request))
	if err != nil {
		t.Fatalf("Failed to write request: %v", err)
	}

	// Read response
	reader := bufio.NewReader(clientConn)
	response, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "action=DUNNO") {
		t.Errorf("Expected action=DUNNO, got %s", response)
	}

	// Close client connection to trigger EOF and exit handler
	clientConn.Close()

	// Wait for handler to exit
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("HandleConnection did not exit after client disconnect")
	}
}

func TestPolicyServer_HandleConnection_MultipleRequests(t *testing.T) {
	mockClient := &MockUserliServiceForPolicy{
		quota: &Quota{PerHour: 100, PerDay: 1000},
	}
	rateLimiter := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}
	server := NewPolicyServer(mockClient, rateLimiter)

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go server.HandleConnection(context.Background(), serverConn)

	reader := bufio.NewReader(clientConn)

	// Send first request
	request1 := "request=smtpd_access_policy\nprotocol_state=END-OF-MESSAGE\nsender=user1@example.org\n\n"
	_, err := clientConn.Write([]byte(request1))
	if err != nil {
		t.Fatalf("Failed to write first request: %v", err)
	}

	// Read first response (two lines: action line + empty line)
	response1, _ := reader.ReadString('\n')
	reader.ReadString('\n') // consume empty line
	if !strings.HasPrefix(response1, "action=DUNNO") {
		t.Errorf("Expected first response action=DUNNO, got %s", response1)
	}

	// Send second request
	request2 := "request=smtpd_access_policy\nprotocol_state=RCPT\nsender=user2@example.org\n\n"
	_, err = clientConn.Write([]byte(request2))
	if err != nil {
		t.Fatalf("Failed to write second request: %v", err)
	}

	// Read second response
	response2, _ := reader.ReadString('\n')
	if !strings.HasPrefix(response2, "action=DUNNO") {
		t.Errorf("Expected second response action=DUNNO, got %s", response2)
	}
}

func TestPolicyServer_StartPolicyServer(t *testing.T) {
	mockClient := &MockUserliServiceForPolicy{
		quota: &Quota{PerHour: 100, PerDay: 1000},
	}
	rateLimiter := &RateLimiter{
		counters: make(map[string]*senderCounter),
	}
	server := NewPolicyServer(mockClient, rateLimiter)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// Start server on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	wg.Add(1)
	go StartPolicyServer(ctx, &wg, addr, server)

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Connect and send request
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	request := "request=smtpd_access_policy\nprotocol_state=END-OF-MESSAGE\nsender=test@example.org\n\n"
	_, _ = conn.Write([]byte(request))

	reader := bufio.NewReader(conn)
	response, _ := reader.ReadString('\n')
	conn.Close()

	if !strings.HasPrefix(response, "action=DUNNO") {
		t.Errorf("Expected action=DUNNO, got %s", response)
	}

	// Shutdown
	cancel()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Fatal("Server did not shutdown within timeout")
	}
}

func TestPolicyServer_ReadRequest_AllFields(t *testing.T) {
	input := `request=smtpd_access_policy
protocol_state=END-OF-MESSAGE
protocol_name=ESMTP
sender=user@example.org
recipient=recipient@example.com
recipient_count=5
client_address=192.168.1.1
client_name=mail.example.org
sasl_method=PLAIN
sasl_username=user@example.org
size=12345
queue_id=ABC123
instance=def456
encryption_cipher=TLS_AES_256_GCM_SHA384

`
	reader := bufio.NewReader(strings.NewReader(input))
	server := &PolicyServer{}

	req, err := server.readRequest(reader)
	if err != nil {
		t.Fatalf("Failed to read request: %v", err)
	}

	// Verify all fields
	if req.Request != "smtpd_access_policy" {
		t.Errorf("Request: got %s, want smtpd_access_policy", req.Request)
	}
	if req.ProtocolState != "END-OF-MESSAGE" {
		t.Errorf("ProtocolState: got %s, want END-OF-MESSAGE", req.ProtocolState)
	}
	if req.ProtocolName != "ESMTP" {
		t.Errorf("ProtocolName: got %s, want ESMTP", req.ProtocolName)
	}
	if req.Sender != "user@example.org" {
		t.Errorf("Sender: got %s, want user@example.org", req.Sender)
	}
	if req.Recipient != "recipient@example.com" {
		t.Errorf("Recipient: got %s, want recipient@example.com", req.Recipient)
	}
	if req.RecipientCount != "5" {
		t.Errorf("RecipientCount: got %s, want 5", req.RecipientCount)
	}
	if req.ClientAddress != "192.168.1.1" {
		t.Errorf("ClientAddress: got %s, want 192.168.1.1", req.ClientAddress)
	}
	if req.ClientName != "mail.example.org" {
		t.Errorf("ClientName: got %s, want mail.example.org", req.ClientName)
	}
	if req.SaslMethod != "PLAIN" {
		t.Errorf("SaslMethod: got %s, want PLAIN", req.SaslMethod)
	}
	if req.SaslUsername != "user@example.org" {
		t.Errorf("SaslUsername: got %s, want user@example.org", req.SaslUsername)
	}
	if req.Size != "12345" {
		t.Errorf("Size: got %s, want 12345", req.Size)
	}
	if req.QueueID != "ABC123" {
		t.Errorf("QueueID: got %s, want ABC123", req.QueueID)
	}
	if req.Instance != "def456" {
		t.Errorf("Instance: got %s, want def456", req.Instance)
	}
	if req.EncryptionCipher != "TLS_AES_256_GCM_SHA384" {
		t.Errorf("EncryptionCipher: got %s, want TLS_AES_256_GCM_SHA384", req.EncryptionCipher)
	}
}

func TestPolicyServer_ReadRequest_InvalidLine(t *testing.T) {
	// Line without equals sign should be skipped
	input := `request=smtpd_access_policy
invalidline
sender=user@example.org

`
	reader := bufio.NewReader(strings.NewReader(input))
	server := &PolicyServer{}

	req, err := server.readRequest(reader)
	if err != nil {
		t.Fatalf("Failed to read request: %v", err)
	}

	if req.Request != "smtpd_access_policy" {
		t.Errorf("Request: got %s, want smtpd_access_policy", req.Request)
	}
	if req.Sender != "user@example.org" {
		t.Errorf("Sender: got %s, want user@example.org", req.Sender)
	}
}
