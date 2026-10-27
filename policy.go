package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// PolicyServer implements a Postfix SMTP Access Policy Delegation server
// for rate limiting outgoing mail based on sender quotas.
type PolicyServer struct {
	client           UserliService
	rateLimiter      *RateLimiter
	rateLimitMessage string
	delimiter        string
	logger           *zap.Logger
}

// PolicyOption configures a PolicyServer
type PolicyOption func(*PolicyServer)

// WithPolicyRecipientDelimiter sets the Postfix recipient delimiter, so that
// "support+tag@example.org" matches the allowed recipient "support@example.org".
func WithPolicyRecipientDelimiter(delimiter string) PolicyOption {
	return func(p *PolicyServer) {
		p.delimiter = delimiter
	}
}

// NewPolicyServer creates a new PolicyServer with the given UserliService
func NewPolicyServer(client UserliService, rateLimiter *RateLimiter, rateLimitMessage string, logger *zap.Logger, opts ...PolicyOption) *PolicyServer {
	p := &PolicyServer{
		client:           client,
		rateLimiter:      rateLimiter,
		rateLimitMessage: rateLimitMessage,
		logger:           logger,
	}
	for _, opt := range opts {
		opt(p)
	}

	return p
}

const (
	// sendingDeniedMessage is returned for recipients an account may not send to.
	sendingDeniedMessage = "Sending is not permitted for this account, please contact support"
	// sendingCheckFailedMessage is returned if the sending permission can't be determined.
	sendingCheckFailedMessage = "Temporary failure, please try again later"
)

// StartPolicyServer starts the policy server on the given address
func StartPolicyServer(ctx context.Context, wg *sync.WaitGroup, addr string, server *PolicyServer) {
	config := TCPServerConfig{
		Name:   "policy",
		Addr:   addr,
		Logger: server.logger,
		OnConnectionAcquired: func() {
			policyActiveConnections.Inc()
		},
		OnConnectionReleased: func() {
			policyActiveConnections.Dec()
		},
		OnConnectionPoolFull: func() {
			policyConnectionPoolFullTotal.Inc()
		},
	}

	StartTCPServer(ctx, wg, config, server)
}

// HandleConnection implements ConnectionHandler interface for PolicyServer
func (p *PolicyServer) HandleConnection(ctx context.Context, conn net.Conn) {
	reader := bufio.NewReader(conn)

	for {
		// Check if context is cancelled
		if ctx.Err() != nil {
			return
		}

		_ = conn.SetReadDeadline(time.Now().Add(ReadTimeout))

		request, err := p.readRequest(reader)
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				return
			}
			p.logger.Debug("Failed to read policy request", zap.Error(err))
			return
		}

		response := p.handleRequest(ctx, request)

		_ = conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
		if err := p.writeResponse(conn, response); err != nil {
			p.logger.Error("Failed to write policy response", zap.Error(err))
			return
		}
	}
}

// PolicyRequest represents a parsed Postfix policy request
type PolicyRequest struct {
	Request          string
	ProtocolState    string
	ProtocolName     string
	Sender           string
	Recipient        string
	RecipientCount   string
	ClientAddress    string
	ClientName       string
	SaslMethod       string
	SaslUsername     string
	Size             string
	QueueID          string
	Instance         string
	EncryptionCipher string
}

// readRequest reads and parses a policy request from the connection
func (p *PolicyServer) readRequest(reader *bufio.Reader) (*PolicyRequest, error) {
	request := &PolicyRequest{}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}

		line = strings.TrimSpace(line)

		// Empty line signals end of request
		if line == "" {
			break
		}

		// Parse name=value pairs
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		name := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch name {
		case "request":
			request.Request = value
		case "protocol_state":
			request.ProtocolState = value
		case "protocol_name":
			request.ProtocolName = value
		case "sender":
			request.Sender = value
		case "recipient":
			request.Recipient = value
		case "recipient_count":
			request.RecipientCount = value
		case "client_address":
			request.ClientAddress = value
		case "client_name":
			request.ClientName = value
		case "sasl_method":
			request.SaslMethod = value
		case "sasl_username":
			request.SaslUsername = value
		case "size":
			request.Size = value
		case "queue_id":
			request.QueueID = value
		case "instance":
			request.Instance = value
		case "encryption_cipher":
			request.EncryptionCipher = value
		}
	}

	return request, nil
}

// handleRequest processes a policy request and returns an action
func (p *PolicyServer) handleRequest(ctx context.Context, req *PolicyRequest) string {
	startTime := time.Now()

	p.logger.Debug("Processing policy request",
		zap.String("queue_id", req.QueueID),
		zap.String("sender", req.Sender),
		zap.String("sasl_username", req.SaslUsername),
		zap.String("protocol", req.ProtocolState))

	// The sending permission is checked per recipient, since only RCPT
	// knows every recipient of a message
	if req.ProtocolState == "RCPT" {
		return p.handleRecipient(ctx, req, startTime)
	}

	// Only check rate limits at END-OF-MESSAGE stage for outgoing mail
	// This ensures we only count messages that will actually be sent
	if req.ProtocolState != "END-OF-MESSAGE" {
		policyRequestsTotal.WithLabelValues("skip", "dunno").Inc()
		policyRequestDuration.WithLabelValues("skip", "dunno").Observe(time.Since(startTime).Seconds())
		return "DUNNO"
	}

	// Use SASL username as the sender identity for rate limiting
	// This is more reliable than the envelope sender for authenticated users
	sender := req.SaslUsername
	if sender == "" {
		sender = req.Sender
	}

	if sender == "" {
		p.logger.Debug("No sender identity found, allowing message")
		policyRequestsTotal.WithLabelValues("check", "dunno").Inc()
		policyRequestDuration.WithLabelValues("check", "dunno").Observe(time.Since(startTime).Seconds())
		return "DUNNO"
	}

	// Fetch quota from Userli API
	quotaCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	quota, err := p.client.GetQuota(quotaCtx, sender)
	if err != nil {
		// API error - fail open (allow the message)
		p.logger.Warn("Failed to fetch quota, allowing message",
			zap.String("queue_id", req.QueueID),
			zap.String("sender", sender), zap.Error(err))
		policyRequestsTotal.WithLabelValues("check", "error").Inc()
		policyRequestDuration.WithLabelValues("check", "error").Observe(time.Since(startTime).Seconds())
		return "DUNNO"
	}

	// No limits configured (both 0 means unlimited)
	if quota.PerHour == 0 && quota.PerDay == 0 {
		p.logger.Debug("No quota limits configured", zap.String("sender", sender))
		policyRequestsTotal.WithLabelValues("check", "dunno").Inc()
		policyRequestDuration.WithLabelValues("check", "dunno").Observe(time.Since(startTime).Seconds())
		return "DUNNO"
	}

	// Check rate limit
	allowed, hourCount, dayCount := p.rateLimiter.CheckAndIncrement(quotaCtx, sender, quota)

	// Update metrics
	quotaChecksTotal.WithLabelValues("checked").Inc()

	if !allowed {
		p.logger.Info("Rate limit exceeded",
			zap.String("queue_id", req.QueueID),
			zap.String("sender", sender),
			zap.Int("hour_count", hourCount),
			zap.Int("day_count", dayCount),
			zap.Int("hour_limit", quota.PerHour),
			zap.Int("day_limit", quota.PerDay))

		policyRequestsTotal.WithLabelValues("check", "reject").Inc()
		policyRequestDuration.WithLabelValues("check", "reject").Observe(time.Since(startTime).Seconds())
		quotaExceededTotal.Inc()

		return "REJECT " + p.rateLimitMessage
	}

	p.logger.Debug("Message allowed",
		zap.String("queue_id", req.QueueID),
		zap.String("sender", sender),
		zap.Int("hour_count", hourCount),
		zap.Int("day_count", dayCount),
		zap.Int("hour_limit", quota.PerHour),
		zap.Int("day_limit", quota.PerDay))

	policyRequestsTotal.WithLabelValues("check", "dunno").Inc()
	policyRequestDuration.WithLabelValues("check", "dunno").Observe(time.Since(startTime).Seconds())

	return "DUNNO"
}

// handleRecipient enforces the sending permission of authenticated senders at
// the RCPT stage. Unlike the rate limit it fails closed: if the permission
// can't be determined, the recipient is deferred. It never counts messages.
func (p *PolicyServer) handleRecipient(ctx context.Context, req *PolicyRequest, startTime time.Time) string {
	observe := func(action string) {
		policyRequestsTotal.WithLabelValues("rcpt", action).Inc()
		policyRequestDuration.WithLabelValues("rcpt", action).Observe(time.Since(startTime).Seconds())
	}

	// Mail from unauthenticated clients is not outgoing mail
	if req.SaslUsername == "" {
		observe("dunno")
		return "DUNNO"
	}

	quotaCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	quota, err := p.client.GetQuota(quotaCtx, req.SaslUsername)
	if err != nil {
		p.logger.Warn("Failed to fetch sending permission, deferring recipient",
			zap.String("queue_id", req.QueueID),
			zap.String("sender", req.SaslUsername), zap.Error(err))
		observe("defer")
		return "DEFER " + sendingCheckFailedMessage
	}

	if quota.SendingAllowed || p.isAllowedRecipient(req.Recipient, quota.AllowedRecipients) {
		observe("dunno")
		return "DUNNO"
	}

	p.logger.Info("Sending denied",
		zap.String("queue_id", req.QueueID),
		zap.String("sender", req.SaslUsername))

	observe("reject")
	sendingDeniedTotal.Inc()

	return "REJECT " + sendingDeniedMessage
}

// isAllowedRecipient reports whether recipient is one of the allowed
// recipients, ignoring case and the recipient delimiter tag.
func (p *PolicyServer) isAllowedRecipient(recipient string, allowed []string) bool {
	recipient = p.normalizeAddress(recipient)
	for _, a := range allowed {
		if recipient == p.normalizeAddress(a) {
			return true
		}
	}

	return false
}

// normalizeAddress lowercases an address and removes the recipient delimiter
// tag from its local part.
func (p *PolicyServer) normalizeAddress(address string) string {
	address = strings.ToLower(strings.TrimSpace(address))
	if p.delimiter == "" {
		return address
	}

	at := strings.LastIndex(address, "@")
	if at < 0 {
		return address
	}

	localPart, domain := address[:at], address[at:]
	if idx := strings.Index(localPart, p.delimiter); idx != -1 {
		localPart = localPart[:idx]
	}

	return localPart + domain
}

// writeResponse writes the policy response to the connection
func (p *PolicyServer) writeResponse(conn net.Conn, action string) error {
	response := fmt.Sprintf("action=%s\n\n", action)
	_, err := conn.Write([]byte(response))
	return err
}
