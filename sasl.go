package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// SASLServer implements the Dovecot SASL authentication protocol.
// It acts as a Dovecot auth server so Postfix can authenticate SMTP
// clients via smtpd_sasl_type=dovecot without requiring Dovecot itself.
type SASLServer struct {
	client UserliService
	logger *zap.Logger
	connID atomic.Uint64
}

// NewSASLServer creates a new SASLServer with the given UserliService.
func NewSASLServer(client UserliService, logger *zap.Logger) *SASLServer {
	return &SASLServer{client: client, logger: logger}
}

// StartSASLServer starts the SASL auth server on the given address.
func StartSASLServer(ctx context.Context, wg *sync.WaitGroup, addr string, server *SASLServer) {
	config := TCPServerConfig{
		Name:   "sasl",
		Addr:   addr,
		Logger: server.logger,
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
	if err := s.sendHandshake(writer, cuid); err != nil {
		s.logger.Error("failed to send handshake", zap.Error(err))
		return
	}

	// Read client handshake
	if err := s.readClientHandshake(reader); err != nil {
		s.logger.Error("failed to read client handshake", zap.Error(err))
		return
	}

	// Handle AUTH requests in a loop (persistent connection)
	for {
		if ctx.Err() != nil {
			return
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")

		parts := strings.Split(line, "\t")
		if len(parts) == 0 {
			continue
		}

		switch parts[0] {
		case "AUTH":
			s.handleAuth(ctx, reader, writer, parts)
		default:
			s.logger.Debug("ignoring unknown command", zap.String("command", parts[0]))
		}
	}
}

// sendHandshake sends the Dovecot auth server handshake.
func (s *SASLServer) sendHandshake(writer *bufio.Writer, cuid uint64) error {
	cookie := make([]byte, 16)
	if _, err := rand.Read(cookie); err != nil {
		return fmt.Errorf("failed to generate cookie: %w", err)
	}

	lines := []string{
		"VERSION\t1\t2",
		fmt.Sprintf("SPID\t%d", os.Getpid()),
		fmt.Sprintf("CUID\t%d", cuid),
		fmt.Sprintf("COOKIE\t%032x", cookie),
		"MECH\tPLAIN\tplaintext",
		"MECH\tLOGIN\tplaintext",
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
func (s *SASLServer) readClientHandshake(reader *bufio.Reader) error {
	gotVersion := false
	gotCPID := false

	for {
		line, err := reader.ReadString('\n')
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

// handleAuth processes an AUTH request and sends OK or FAIL.
func (s *SASLServer) handleAuth(ctx context.Context, reader *bufio.Reader, writer *bufio.Writer, parts []string) {
	// AUTH\t<id>\t<mechanism>\t[params...]
	if len(parts) < 3 {
		s.writeResponse(writer, "FAIL\t0\treason=Invalid AUTH request")
		return
	}

	id := parts[1]
	mechanism := strings.ToUpper(parts[2])

	startTime := time.Now()

	var email, password string
	var err error

	switch mechanism {
	case "PLAIN":
		email, password, err = s.parsePlainAuth(parts)
	case "LOGIN":
		email, password, err = s.handleLoginAuth(id, reader, writer)
	default:
		s.recordAuthResult(mechanism, "error", startTime)
		s.writeResponse(writer, fmt.Sprintf("FAIL\t%s\treason=Unsupported mechanism", id))
		return
	}

	if err != nil {
		s.logger.Info("auth parsing failed", zap.String("mechanism", mechanism), zap.Error(err))
		s.recordAuthResult(mechanism, "error", startTime)
		s.writeResponse(writer, fmt.Sprintf("FAIL\t%s\treason=%s", id, err.Error()))
		return
	}

	s.authenticateAndRespond(ctx, writer, id, mechanism, email, password, startTime)
}

// parsePlainAuth extracts email and password from a PLAIN AUTH request.
// The resp= parameter contains base64-encoded "\x00user\x00password".
func (s *SASLServer) parsePlainAuth(parts []string) (string, string, error) {
	var respB64 string

	for _, p := range parts[3:] {
		if strings.HasPrefix(p, "resp=") {
			respB64 = strings.TrimPrefix(p, "resp=")
			break
		}
	}

	if respB64 == "" {
		return "", "", fmt.Errorf("missing resp= parameter")
	}

	decoded, err := base64.StdEncoding.DecodeString(respB64)
	if err != nil {
		return "", "", fmt.Errorf("invalid base64 in resp=")
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

// handleLoginAuth handles the LOGIN mechanism using CONT roundtrips.
//
// Flow:
//
//	S: CONT\t<id>\t<base64("Username:")>
//	C: CONT\t<id>\t<base64(username)>
//	S: CONT\t<id>\t<base64("Password:")>
//	C: CONT\t<id>\t<base64(password)>
func (s *SASLServer) handleLoginAuth(id string, reader *bufio.Reader, writer *bufio.Writer) (string, string, error) {
	// Send Username challenge
	usernameChallenge := base64.StdEncoding.EncodeToString([]byte("Username:"))
	s.writeResponse(writer, fmt.Sprintf("CONT\t%s\t%s", id, usernameChallenge))

	// Read username response
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("failed to read LOGIN username: %w", err)
	}
	line = strings.TrimRight(line, "\r\n")

	contParts := strings.SplitN(line, "\t", 3)
	if len(contParts) < 3 || contParts[0] != "CONT" {
		return "", "", fmt.Errorf("expected CONT response for username")
	}

	usernameBytes, err := base64.StdEncoding.DecodeString(contParts[2])
	if err != nil {
		return "", "", fmt.Errorf("invalid base64 in LOGIN username")
	}
	email := string(usernameBytes)

	// Send Password challenge
	passwordChallenge := base64.StdEncoding.EncodeToString([]byte("Password:"))
	s.writeResponse(writer, fmt.Sprintf("CONT\t%s\t%s", id, passwordChallenge))

	// Read password response
	line, err = reader.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("failed to read LOGIN password: %w", err)
	}
	line = strings.TrimRight(line, "\r\n")

	contParts = strings.SplitN(line, "\t", 3)
	if len(contParts) < 3 || contParts[0] != "CONT" {
		return "", "", fmt.Errorf("expected CONT response for password")
	}

	passwordBytes, err := base64.StdEncoding.DecodeString(contParts[2])
	if err != nil {
		return "", "", fmt.Errorf("invalid base64 in LOGIN password")
	}
	password := string(passwordBytes)

	if email == "" || password == "" {
		return "", "", fmt.Errorf("empty username or password")
	}

	return email, password, nil
}

// authenticateAndRespond calls the Userli API and writes the auth result.
func (s *SASLServer) authenticateAndRespond(ctx context.Context, writer *bufio.Writer, id, mechanism, email, password string, startTime time.Time) {
	ok, message, err := s.client.Authenticate(ctx, email, password)
	if err != nil {
		// Fail-closed: API errors reject authentication
		s.logger.Error("authentication API error",
			zap.String("email", email), zap.Error(err))
		s.recordAuthResult(mechanism, "error", startTime)
		s.writeResponse(writer, fmt.Sprintf("FAIL\t%s\tuser=%s\treason=Internal error", id, email))
		return
	}

	if ok {
		s.logger.Info("authentication successful", zap.String("email", email))
		s.recordAuthResult(mechanism, "success", startTime)
		s.writeResponse(writer, fmt.Sprintf("OK\t%s\tuser=%s", id, email))
	} else {
		reason := message
		if reason == "" {
			reason = "authentication failed"
		}
		s.logger.Info("authentication failed", zap.String("email", email), zap.String("reason", reason))
		s.recordAuthResult(mechanism, "invalid_credentials", startTime)
		s.writeResponse(writer, fmt.Sprintf("FAIL\t%s\tuser=%s\treason=%s", id, email, reason))
	}
}

// writeResponse writes a line to the writer and flushes.
func (s *SASLServer) writeResponse(writer *bufio.Writer, line string) {
	if _, err := writer.WriteString(line + "\n"); err != nil {
		s.logger.Error("failed to write response", zap.Error(err))
	}
	if err := writer.Flush(); err != nil {
		s.logger.Error("failed to flush response", zap.Error(err))
	}
}

// recordAuthResult records metrics for an authentication attempt.
func (s *SASLServer) recordAuthResult(mechanism, result string, startTime time.Time) {
	duration := time.Since(startTime).Seconds()
	saslAuthTotal.WithLabelValues(mechanism, result).Inc()
	saslAuthDuration.WithLabelValues(mechanism, result).Observe(duration)
}
