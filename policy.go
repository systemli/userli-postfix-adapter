package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

// PolicyServer implements a Postfix SMTP Access Policy Delegation server
// for rate limiting outgoing mail based on sender quotas.
type PolicyServer struct {
	client      UserliService
	rateLimiter *RateLimiter
}

// NewPolicyServer creates a new PolicyServer with the given UserliService
func NewPolicyServer(client UserliService, rateLimiter *RateLimiter) *PolicyServer {
	return &PolicyServer{
		client:      client,
		rateLimiter: rateLimiter,
	}
}

// StartPolicyServer starts the policy server on the given address
func StartPolicyServer(ctx context.Context, wg *sync.WaitGroup, addr string, server *PolicyServer) {
	config := TCPServerConfig{
		Name: "policy",
		Addr: addr,
		OnConnectionAcquired: func() {
			policyActiveConnections.Inc()
		},
		OnConnectionReleased: func() {
			policyActiveConnections.Dec()
		},
	}

	StartTCPServer(ctx, wg, config, server)
}

// HandleConnection implements ConnectionHandler interface for PolicyServer
func (p *PolicyServer) HandleConnection(ctx context.Context, conn net.Conn) {
	reader := bufio.NewReader(conn)

	for {
		_ = conn.SetReadDeadline(time.Now().Add(ReadTimeout))

		request, err := p.readRequest(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				logger.Debug("Failed to read policy request", zap.Error(err))
			}
			return
		}

		response := p.handleRequest(ctx, request)

		_ = conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
		if err := p.writeResponse(conn, response); err != nil {
			logger.Error("Failed to write policy response", zap.Error(err))
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

	logger.Debug("Processing policy request",
		zap.String("sender", req.Sender),
		zap.String("sasl_username", req.SaslUsername),
		zap.String("protocol", req.ProtocolState))

	// Only check at END-OF-MESSAGE stage for outgoing mail
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
		logger.Debug("No sender identity found, allowing message")
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
		logger.Warn("Failed to fetch quota, allowing message",
			zap.String("sender", sender), zap.Error(err))
		policyRequestsTotal.WithLabelValues("check", "error").Inc()
		policyRequestDuration.WithLabelValues("check", "error").Observe(time.Since(startTime).Seconds())
		return "DUNNO"
	}

	// No limits configured (both 0 means unlimited)
	if quota.PerHour == 0 && quota.PerDay == 0 {
		logger.Debug("No quota limits configured", zap.String("sender", sender))
		policyRequestsTotal.WithLabelValues("check", "dunno").Inc()
		policyRequestDuration.WithLabelValues("check", "dunno").Observe(time.Since(startTime).Seconds())
		return "DUNNO"
	}

	// Check rate limit
	allowed, hourCount, dayCount := p.rateLimiter.CheckAndIncrement(sender, quota)

	// Update metrics
	quotaChecksTotal.WithLabelValues("checked").Inc()

	if !allowed {
		logger.Info("Rate limit exceeded",
			zap.String("sender", sender),
			zap.Int("hour_count", hourCount),
			zap.Int("day_count", dayCount),
			zap.Int("hour_limit", quota.PerHour),
			zap.Int("day_limit", quota.PerDay))

		policyRequestsTotal.WithLabelValues("check", "reject").Inc()
		policyRequestDuration.WithLabelValues("check", "reject").Observe(time.Since(startTime).Seconds())
		quotaExceededTotal.Inc()

		return "REJECT Rate limit exceeded, please try again later"
	}

	logger.Debug("Message allowed",
		zap.String("sender", sender),
		zap.Int("hour_count", hourCount),
		zap.Int("day_count", dayCount),
		zap.Int("hour_limit", quota.PerHour),
		zap.Int("day_limit", quota.PerDay))

	policyRequestsTotal.WithLabelValues("check", "dunno").Inc()
	policyRequestDuration.WithLabelValues("check", "dunno").Observe(time.Since(startTime).Seconds())

	return "DUNNO"
}

// writeResponse writes the policy response to the connection
func (p *PolicyServer) writeResponse(conn net.Conn, action string) error {
	response := fmt.Sprintf("action=%s\n\n", action)
	_, err := conn.Write([]byte(response))
	return err
}
