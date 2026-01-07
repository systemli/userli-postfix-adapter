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

	log "github.com/sirupsen/logrus"
)

// PolicyServer implements a Postfix SMTP Access Policy Delegation server
// for rate limiting outgoing mail based on sender quotas.
type PolicyServer struct {
	client      UserliService
	rateLimiter *RateLimiter
	ctx         context.Context
}

// NewPolicyServer creates a new PolicyServer with the given UserliService
func NewPolicyServer(ctx context.Context, client UserliService, rateLimiter *RateLimiter) *PolicyServer {
	return &PolicyServer{
		client:      client,
		rateLimiter: rateLimiter,
		ctx:         ctx,
	}
}

// StartPolicyServer starts the policy server on the given address
func StartPolicyServer(ctx context.Context, wg *sync.WaitGroup, addr string, server *PolicyServer) {
	defer wg.Done()

	connSemaphore := make(chan struct{}, MaxConcurrentConnections)
	var activeConnWg sync.WaitGroup

	lc := net.ListenConfig{
		KeepAlive: KeepAliveTimeout,
	}

	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		log.WithError(err).WithField("addr", addr).Error("Failed to create policy listener")
		return
	}
	defer listener.Close()

	// Graceful shutdown handler
	go func() {
		<-ctx.Done()
		log.WithField("addr", addr).Info("Shutting down policy server...")
		listener.Close()
		activeConnWg.Wait()
		log.WithField("addr", addr).Info("All policy connections closed")
	}()

	log.WithField("addr", addr).Info("Policy server started")

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.WithError(err).WithField("addr", addr).Error("Accept failed")
			continue
		}

		select {
		case connSemaphore <- struct{}{}:
			activeConnWg.Add(1)
			policyActiveConnections.Inc()
			go server.handleConnection(conn, connSemaphore, &activeConnWg)
		default:
			log.WithField("addr", addr).Warn("Connection pool full, rejecting policy connection")
			conn.Close()
		}
	}
}

// handleConnection processes a single policy connection
func (p *PolicyServer) handleConnection(conn net.Conn, semaphore chan struct{}, wg *sync.WaitGroup) {
	defer func() {
		conn.Close()
		<-semaphore
		policyActiveConnections.Dec()
		wg.Done()
	}()

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(KeepAliveTimeout)
	}

	deadline := time.Now().Add(ConnectionTimeout)
	_ = conn.SetDeadline(deadline)

	reader := bufio.NewReader(conn)

	for {
		_ = conn.SetReadDeadline(time.Now().Add(ReadTimeout))

		request, err := p.readRequest(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.WithError(err).Debug("Failed to read policy request")
			}
			return
		}

		response := p.handleRequest(request)

		_ = conn.SetWriteDeadline(time.Now().Add(WriteTimeout))
		if err := p.writeResponse(conn, response); err != nil {
			log.WithError(err).Error("Failed to write policy response")
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
func (p *PolicyServer) handleRequest(req *PolicyRequest) string {
	startTime := time.Now()

	log.WithFields(log.Fields{
		"sender":        req.Sender,
		"sasl_username": req.SaslUsername,
		"protocol":      req.ProtocolState,
	}).Debug("Processing policy request")

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
		log.Debug("No sender identity found, allowing message")
		policyRequestsTotal.WithLabelValues("check", "dunno").Inc()
		policyRequestDuration.WithLabelValues("check", "dunno").Observe(time.Since(startTime).Seconds())
		return "DUNNO"
	}

	// Fetch quota from Userli API
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
	defer cancel()

	quota, err := p.client.GetQuota(ctx, sender)
	if err != nil {
		// API error - fail open (allow the message)
		log.WithError(err).WithField("sender", sender).Warn("Failed to fetch quota, allowing message")
		policyRequestsTotal.WithLabelValues("check", "error").Inc()
		policyRequestDuration.WithLabelValues("check", "error").Observe(time.Since(startTime).Seconds())
		return "DUNNO"
	}

	// No limits configured (both 0 means unlimited)
	if quota.PerHour == 0 && quota.PerDay == 0 {
		log.WithField("sender", sender).Debug("No quota limits configured")
		policyRequestsTotal.WithLabelValues("check", "dunno").Inc()
		policyRequestDuration.WithLabelValues("check", "dunno").Observe(time.Since(startTime).Seconds())
		return "DUNNO"
	}

	// Check rate limit
	allowed, hourCount, dayCount := p.rateLimiter.CheckAndIncrement(sender, quota)

	// Update metrics
	quotaChecksTotal.WithLabelValues("checked").Inc()

	if !allowed {
		log.WithFields(log.Fields{
			"sender":     sender,
			"hour_count": hourCount,
			"day_count":  dayCount,
			"hour_limit": quota.PerHour,
			"day_limit":  quota.PerDay,
		}).Info("Rate limit exceeded")

		policyRequestsTotal.WithLabelValues("check", "reject").Inc()
		policyRequestDuration.WithLabelValues("check", "reject").Observe(time.Since(startTime).Seconds())
		quotaExceededTotal.Inc()

		return "REJECT Rate limit exceeded, please try again later"
	}

	log.WithFields(log.Fields{
		"sender":     sender,
		"hour_count": hourCount,
		"day_count":  dayCount,
		"hour_limit": quota.PerHour,
		"day_limit":  quota.PerDay,
	}).Debug("Message allowed")

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
