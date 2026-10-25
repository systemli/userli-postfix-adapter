package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// LOGIN mechanism challenges, base64-encoded.
var (
	loginUsernameChallenge = base64.StdEncoding.EncodeToString([]byte("Username:"))
	loginPasswordChallenge = base64.StdEncoding.EncodeToString([]byte("Password:"))
)

// saslMaxLineLength bounds a single protocol line. Postfix limits SMTP
// AUTH lines far below this, so only a misbehaving client on the socket
// can exceed it.
const saslMaxLineLength = 16 * 1024

// errLineTooLong is returned when a protocol line exceeds saslMaxLineLength.
var errLineTooLong = errors.New("line too long")

// saslFailReason is the only reason reported for rejected credentials.
// smtpd passes it verbatim to the SMTP client ("535 5.7.8 Error:
// authentication failed: <reason>"), and Userli reports account states
// such as a spam lock before checking the password, so anything more
// specific would reveal them to unauthenticated clients.
const saslFailReason = "authentication failed"

// SASLServer implements the Dovecot SASL authentication protocol.
// It acts as a Dovecot auth server so Postfix can authenticate SMTP
// clients via smtpd_sasl_type=dovecot without requiring Dovecot itself.
type SASLServer struct {
	client UserliService
	logger *zap.Logger
	connID atomic.Uint64
	// requireTLS rejects AUTH requests from SMTP sessions without TLS,
	// like Dovecot's disable_plaintext_auth.
	requireTLS bool
}

// NewSASLServer creates a new SASLServer with the given UserliService.
func NewSASLServer(client UserliService, logger *zap.Logger, requireTLS bool) *SASLServer {
	return &SASLServer{client: client, logger: logger, requireTLS: requireTLS}
}

// StartSASLServer starts the SASL auth server on the given address.
// socketMode applies to UNIX sockets; zero means DefaultSocketMode.
func StartSASLServer(ctx context.Context, wg *sync.WaitGroup, addr string, socketMode os.FileMode, server *SASLServer) {
	config := TCPServerConfig{
		Name:       "sasl",
		Addr:       addr,
		Logger:     server.logger,
		SocketMode: socketMode,
		OnConnectionAcquired: func() {
			saslActiveConnections.Inc()
		},
		OnConnectionReleased: func() {
			saslActiveConnections.Dec()
		},
		OnConnectionPoolFull: func() {
			saslConnectionPoolFullTotal.Inc()
		},
	}

	StartTCPServer(ctx, wg, config, server)
}

// HandleConnection implements the ConnectionHandler interface.
// It performs the Dovecot auth protocol handshake and then handles
// AUTH requests in a loop (persistent connections).
func (s *SASLServer) HandleConnection(ctx context.Context, conn net.Conn) {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	cuid := s.connID.Add(1)

	// Send server handshake
	_ = conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
	if err := s.sendHandshake(writer, cuid); err != nil {
		s.logger.Error("failed to send handshake", zap.Error(err))
		return
	}

	// Read client handshake
	if err := s.readClientHandshake(conn, reader); err != nil {
		s.logger.Error("failed to read client handshake", zap.Error(err))
		return
	}

	// Handle AUTH requests in a loop (persistent connection). Postfix'
	// xsasl_dovecot client keeps the auth socket open for the lifetime of
	// the smtpd process, which can be idle for any time between two AUTHs.
	// It only reconnects if writing the AUTH fails, and on a TCP socket
	// writing to a connection we closed succeeds, so any idle timeout here
	// ends in `454 Connection lost to authentication server`. Wait without
	// a deadline and close the connection on shutdown instead.
	//
	// Multi-step mechanisms are tracked in pending instead of blocking on
	// the next line: smtpd never tells the auth server when an SMTP client
	// aborts (`*`) or disconnects mid-exchange, so the next line on this
	// socket may be a fresh AUTH from another session of the same smtpd
	// process rather than the CONT we asked for.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetReadDeadline(time.Time{})

	var pending *pendingAuth
	for {
		if ctx.Err() != nil {
			return
		}

		line, err := readLine(reader)
		if err != nil {
			s.logger.Debug("failed to read AUTH request", zap.Error(err))
			return
		}
		line = strings.TrimRight(line, "\r\n")

		parts := strings.Split(line, "\t")
		if len(parts) == 0 {
			continue
		}

		switch parts[0] {
		case "AUTH":
			if pending != nil {
				s.logger.Debug("abandoning unfinished auth request",
					zap.String("id", pending.id), zap.String("mechanism", pending.mechanism))
				s.recordAuthResult(pending.mechanism, "aborted", pending.startTime)
			}
			pending = s.handleAuth(ctx, conn, writer, parts)
		case "CONT":
			pending = s.handleCont(ctx, conn, writer, pending, parts)
		default:
			s.logger.Debug("ignoring unknown command", zap.String("command", parts[0]))
		}
	}
}

// pendingAuth is an AUTH request waiting for a CONT from the client.
type pendingAuth struct {
	id        string
	mechanism string
	// clientIP is the SMTP client address Postfix sends as rip=.
	clientIP string
	// username is set once the LOGIN mechanism has received it.
	username  string
	startTime time.Time
}

// sendHandshake sends the Dovecot auth server handshake.
func (s *SASLServer) sendHandshake(writer *bufio.Writer, cuid uint64) error {
	cookie := make([]byte, 16)
	if _, err := rand.Read(cookie); err != nil {
		return fmt.Errorf("failed to generate cookie: %w", err)
	}

	// MECH lines must precede SPID: Postfix uses SPID-before-MECH as the
	// signal that it has connected to an auth-master socket and aborts.
	lines := []string{
		"VERSION\t1\t2",
		"MECH\tPLAIN\tplaintext",
		"MECH\tLOGIN\tplaintext",
		fmt.Sprintf("SPID\t%d", os.Getpid()),
		fmt.Sprintf("CUID\t%d", cuid),
		fmt.Sprintf("COOKIE\t%032x", cookie),
		"DONE",
	}

	for _, line := range lines {
		if _, err := writer.WriteString(line + "\n"); err != nil {
			return err
		}
	}

	return writer.Flush()
}

// readClientHandshake reads and validates the client (Postfix) handshake.
func (s *SASLServer) readClientHandshake(conn net.Conn, reader *bufio.Reader) error {
	gotVersion := false
	gotCPID := false

	for {
		_ = conn.SetReadDeadline(time.Now().Add(ReadTimeout))
		line, err := readLine(reader)
		if err != nil {
			return fmt.Errorf("failed to read client handshake: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")

		parts := strings.Split(line, "\t")
		if len(parts) == 0 {
			continue
		}

		switch parts[0] {
		case "VERSION":
			if len(parts) < 3 {
				return fmt.Errorf("invalid VERSION line: %s", line)
			}
			if parts[1] != "1" {
				return fmt.Errorf("unsupported major version: %s", parts[1])
			}
			gotVersion = true
		case "CPID":
			gotCPID = true
		}

		// Postfix sends VERSION then CPID, and CPID terminates the handshake
		if gotVersion && gotCPID {
			return nil
		}
	}
}

// handleAuth processes an AUTH request. It either answers with OK/FAIL
// right away or sends a CONT challenge and returns the request state
// that the following CONT continues.
func (s *SASLServer) handleAuth(ctx context.Context, conn net.Conn, writer *bufio.Writer, parts []string) *pendingAuth {
	// AUTH\t<id>\t<mechanism>\t[params...]
	if len(parts) < 3 {
		s.writeResponse(conn, writer, "FAIL\t0\treason=Invalid AUTH request")
		return nil
	}

	req := &pendingAuth{
		id:        parts[1],
		mechanism: strings.ToUpper(parts[2]),
		startTime: time.Now(),
	}

	// Postfix sends resp= only if the SMTP client supplied an initial
	// response, and forwards it unchanged. RFC 4954 encodes a zero-length
	// initial response as "=". secured is set when the SMTP session uses TLS.
	var initialResponse string
	hasInitialResponse := false
	secured := false
	for _, p := range parts[3:] {
		if v, ok := strings.CutPrefix(p, "resp="); ok {
			initialResponse = v
			hasInitialResponse = true
		} else if v, ok := strings.CutPrefix(p, "rip="); ok {
			req.clientIP = v
		} else if p == "secured" {
			secured = true
		}
	}
	if initialResponse == "=" {
		initialResponse = ""
	}

	// Checked before any challenge, so the client never sends the
	// password over the unencrypted session.
	if s.requireTLS && !secured {
		s.logger.Info("authentication without TLS rejected",
			zap.String("mechanism", req.mechanism), zap.String("client", req.clientIP))
		s.recordAuthResult(req.mechanism, "insecure", req.startTime)
		s.writeResponse(conn, writer, fmt.Sprintf("FAIL\t%s\treason=Plaintext authentication disallowed without TLS", req.id))
		return nil
	}

	switch req.mechanism {
	case "PLAIN":
		if !hasInitialResponse {
			// RFC 4616: request the credentials with an empty challenge.
			s.writeResponse(conn, writer, fmt.Sprintf("CONT\t%s\t", req.id))
			return req
		}
		s.finishPlain(ctx, conn, writer, req, initialResponse)
		return nil
	case "LOGIN":
		if !hasInitialResponse {
			s.writeResponse(conn, writer, fmt.Sprintf("CONT\t%s\t%s", req.id, loginUsernameChallenge))
			return req
		}
		return s.continueLogin(ctx, conn, writer, req, initialResponse)
	default:
		s.recordAuthResult(req.mechanism, "error", req.startTime)
		s.writeResponse(conn, writer, fmt.Sprintf("FAIL\t%s\treason=Unsupported mechanism", req.id))
		return nil
	}
}

// handleCont processes a CONT line for the pending request and returns the
// request state that remains pending afterwards.
func (s *SASLServer) handleCont(ctx context.Context, conn net.Conn, writer *bufio.Writer, req *pendingAuth, parts []string) *pendingAuth {
	// CONT\t<id>\t<base64 data>
	if len(parts) < 3 {
		s.writeResponse(conn, writer, "FAIL\t0\treason=Invalid CONT request")
		return req
	}

	id := parts[1]
	if req == nil || req.id != id {
		// Answer anyway: Postfix blocks until it reads a reply for this id.
		s.logger.Debug("CONT for unknown auth request", zap.String("id", id))
		s.writeResponse(conn, writer, fmt.Sprintf("FAIL\t%s\treason=Unexpected CONT", sanitizeField(id)))
		return req
	}

	data := parts[2]
	if data == "*" {
		s.recordAuthResult(req.mechanism, "aborted", req.startTime)
		s.writeResponse(conn, writer, fmt.Sprintf("FAIL\t%s\treason=Authentication aborted", req.id))
		return nil
	}

	switch req.mechanism {
	case "PLAIN":
		s.finishPlain(ctx, conn, writer, req, data)
		return nil
	case "LOGIN":
		return s.continueLogin(ctx, conn, writer, req, data)
	default:
		return nil
	}
}

// finishPlain decodes a PLAIN response and authenticates it.
func (s *SASLServer) finishPlain(ctx context.Context, conn net.Conn, writer *bufio.Writer, req *pendingAuth, respB64 string) {
	email, password, err := parsePlainResponse(respB64)
	if err != nil {
		s.failParse(conn, writer, req, err)
		return
	}

	s.authenticateAndRespond(ctx, conn, writer, req, email, password)
}

// continueLogin advances the LOGIN mechanism by one step.
//
// Flow:
//
//	S: CONT\t<id>\t<base64("Username:")>   (skipped if AUTH carried resp=)
//	C: CONT\t<id>\t<base64(username)>
//	S: CONT\t<id>\t<base64("Password:")>
//	C: CONT\t<id>\t<base64(password)>
func (s *SASLServer) continueLogin(ctx context.Context, conn net.Conn, writer *bufio.Writer, req *pendingAuth, dataB64 string) *pendingAuth {
	decoded, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		s.failParse(conn, writer, req, fmt.Errorf("invalid base64 in LOGIN response"))
		return nil
	}

	if req.username == "" {
		if len(decoded) == 0 {
			s.failParse(conn, writer, req, fmt.Errorf("empty username or password"))
			return nil
		}
		req.username = string(decoded)
		s.writeResponse(conn, writer, fmt.Sprintf("CONT\t%s\t%s", req.id, loginPasswordChallenge))
		return req
	}

	if len(decoded) == 0 {
		s.failParse(conn, writer, req, fmt.Errorf("empty username or password"))
		return nil
	}

	s.authenticateAndRespond(ctx, conn, writer, req, req.username, string(decoded))
	return nil
}

// failParse rejects a request whose client data could not be parsed.
func (s *SASLServer) failParse(conn net.Conn, writer *bufio.Writer, req *pendingAuth, err error) {
	s.logger.Info("auth parsing failed",
		zap.String("mechanism", req.mechanism), zap.String("client", req.clientIP), zap.Error(err))
	s.recordAuthResult(req.mechanism, "error", req.startTime)
	s.writeResponse(conn, writer, fmt.Sprintf("FAIL\t%s\treason=%s", req.id, saslFailReason))
}

// parsePlainResponse extracts email and password from a base64-encoded
// PLAIN response ("\x00user\x00password").
func parsePlainResponse(respB64 string) (string, string, error) {
	decoded, err := base64.StdEncoding.DecodeString(respB64)
	if err != nil {
		return "", "", fmt.Errorf("invalid base64 in PLAIN response")
	}

	// RFC 4616: authzid \x00 authcid \x00 passwd
	// authzid is typically empty, so: \x00user\x00password
	nullParts := strings.SplitN(string(decoded), "\x00", 3)
	if len(nullParts) != 3 {
		return "", "", fmt.Errorf("invalid PLAIN payload format")
	}

	email := nullParts[1]
	password := nullParts[2]

	if email == "" || password == "" {
		return "", "", fmt.Errorf("empty username or password")
	}

	return email, password, nil
}

// authenticateAndRespond calls the Userli API and writes the auth result.
func (s *SASLServer) authenticateAndRespond(ctx context.Context, conn net.Conn, writer *bufio.Writer, req *pendingAuth, email, password string) {
	result, err := s.client.Authenticate(ctx, email, password)
	if err != nil {
		// Fail-closed, but as a temporary failure: Postfix ignores the
		// Dovecot "temp" flag and answers every FAIL with a permanent 535,
		// which makes mail clients ask for a new password. Only a lost
		// connection makes smtpd reply 454; it reconnects on the next AUTH.
		s.logger.Error("authentication API error",
			zap.String("email", email), zap.String("client", req.clientIP), zap.Error(err))
		s.recordAuthResult(req.mechanism, "error", req.startTime)
		_ = conn.Close()
		return
	}

	if result.OK {
		// Report the canonical login rather than what the client typed.
		// Postfix exposes user= as sasl_username, which keys the
		// rate limiter and reject_sender_login_mismatch; variants like
		// "User@example.org" or "user+tag@example.org" must not yield
		// distinct identities.
		username := result.Username
		if username == "" {
			username = email
		}
		s.logger.Info("authentication successful",
			zap.String("email", username), zap.String("client", req.clientIP))
		s.recordAuthResult(req.mechanism, "success", req.startTime)
		s.writeResponse(conn, writer, fmt.Sprintf("OK\t%s\tuser=%s", req.id, sanitizeField(username)))
	} else {
		s.logger.Info("authentication failed",
			zap.String("email", email), zap.String("client", req.clientIP), zap.String("reason", result.Message))
		s.recordAuthResult(req.mechanism, "invalid_credentials", req.startTime)
		s.writeResponse(conn, writer, fmt.Sprintf("FAIL\t%s\tuser=%s\treason=%s", req.id, sanitizeField(email), saslFailReason))
	}
}

// readLine reads a single \n-terminated line of at most saslMaxLineLength
// bytes, including the terminator.
func readLine(reader *bufio.Reader) (string, error) {
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(line)+len(chunk) > saslMaxLineLength {
			return "", errLineTooLong
		}
		line = append(line, chunk...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return "", err
		}
		return string(line), nil
	}
}

// writeResponse writes a line to the writer and flushes.
func (s *SASLServer) writeResponse(conn net.Conn, writer *bufio.Writer, line string) {
	_ = conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
	if _, err := writer.WriteString(line + "\n"); err != nil {
		s.logger.Error("failed to write response", zap.Error(err))
	}
	if err := writer.Flush(); err != nil {
		s.logger.Error("failed to flush response", zap.Error(err))
	}
}

// sanitizeField removes characters that would break the Dovecot auth
// protocol's tab/newline framing. Postfix treats reason/user as opaque
// text up to the next field separator.
func sanitizeField(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\n', '\r':
			return ' '
		default:
			return r
		}
	}, s)
}

// recordAuthResult records metrics for an authentication attempt.
func (s *SASLServer) recordAuthResult(mechanism, result string, startTime time.Time) {
	duration := time.Since(startTime).Seconds()
	saslAuthTotal.WithLabelValues(mechanism, result).Inc()
	saslAuthDuration.WithLabelValues(mechanism, result).Observe(duration)
}
