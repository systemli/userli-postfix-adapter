package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/markdingo/netstring"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

// SocketmapResponse represents a socketmap protocol response
type SocketmapResponse struct {
	Status string
	Data   string
}

// String returns the response as a socketmap protocol string
func (r *SocketmapResponse) String() string {
	if r.Data == "" {
		return r.Status
	}
	return fmt.Sprintf("%s %s", r.Status, r.Data)
}

// LookupServer handles Postfix socketmap protocol requests for lookups.
// It implements the ConnectionHandler interface.
type LookupServer struct {
	client UserliService
}

// NewLookupServer creates a new LookupServer with the given UserliService
func NewLookupServer(client UserliService) *LookupServer {
	return &LookupServer{client: client}
}

// StartLookupServer starts the lookup server on the given address
func StartLookupServer(ctx context.Context, wg *sync.WaitGroup, addr string, server *LookupServer) {
	config := TCPServerConfig{
		Name: "socketmap",
		Addr: addr,
		OnConnectionAcquired: func() {
			activeConnections.Inc()
		},
		OnConnectionReleased: func() {
			activeConnections.Dec()
		},
		OnPoolUsageChanged: func(size int) {
			connectionPoolUsage.Set(float64(size))
		},
	}

	StartTCPServer(ctx, wg, config, server)
}

// HandleConnection implements ConnectionHandler interface for LookupServer.
// It processes socketmap protocol requests, supporting persistent connections with multiple requests.
// Note: The caller (tcpserver.go) is responsible for closing the connection.
func (s *LookupServer) HandleConnection(ctx context.Context, conn net.Conn) {
	decoder := netstring.NewDecoder(conn)
	encoder := netstring.NewEncoder(conn)

	for {
		// Check if context is cancelled
		if ctx.Err() != nil {
			return
		}

		// Set read deadline for each request
		_ = conn.SetReadDeadline(time.Now().Add(ReadTimeout))

		// Read the request netstring
		requestBytes, err := decoder.Decode()
		if err != nil {
			// Check if this is a normal connection closure (EOF) or an actual error
			if err == io.EOF {
				logger.Debug("Client closed connection")
			} else {
				logger.Debug("Failed to decode request", zap.Error(err))
			}
			return
		}
		request := string(requestBytes)

		now := time.Now()

		// Parse the request: "name key"
		parts := strings.SplitN(strings.TrimSpace(request), " ", 2)
		if len(parts) != 2 {
			logger.Error("Invalid request format", zap.String("request", request))
			response := &SocketmapResponse{Status: "PERM", Data: "Invalid request format"}
			s.writeResponse(encoder, conn, response, now, "invalid")
			continue
		}

		mapName := parts[0]
		key := parts[1]

		logger.Debug("Processing socketmap request",
			zap.String("map", mapName),
			zap.String("key", key))

		response := s.processRequest(ctx, mapName, key)
		s.writeResponse(encoder, conn, response, now, mapName)
	}
}

// processRequest routes a socketmap request to the appropriate handler with a timeout context.
// Using a separate method ensures defer cancel() runs after each request, preventing context leaks.
func (s *LookupServer) processRequest(ctx context.Context, mapName, key string) *SocketmapResponse {
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	switch mapName {
	case "alias":
		return s.handleAlias(reqCtx, key)
	case "domain":
		return s.handleDomain(reqCtx, key)
	case "mailbox":
		return s.handleMailbox(reqCtx, key)
	case "senders":
		return s.handleSenders(reqCtx, key)
	default:
		logger.Error("Unknown map name", zap.String("map", mapName))
		return &SocketmapResponse{Status: "PERM", Data: "Unknown map name"}
	}
}

// handleAlias processes alias lookup requests
func (s *LookupServer) handleAlias(ctx context.Context, key string) *SocketmapResponse {
	aliases, err := s.client.GetAliases(ctx, key)
	if err != nil {
		logger.Error("Error fetching aliases", zap.String("key", key), zap.Error(err))
		return &SocketmapResponse{Status: "TEMP", Data: "Error fetching aliases"}
	}

	if len(aliases) == 0 {
		return &SocketmapResponse{Status: "NOTFOUND"}
	}

	return &SocketmapResponse{Status: "OK", Data: strings.Join(aliases, ",")}
}

// handleDomain processes domain lookup requests
func (s *LookupServer) handleDomain(ctx context.Context, key string) *SocketmapResponse {
	exists, err := s.client.GetDomain(ctx, key)
	if err != nil {
		logger.Error("Error fetching domain", zap.String("key", key), zap.Error(err))
		return &SocketmapResponse{Status: "TEMP", Data: "Error fetching domain"}
	}

	if !exists {
		return &SocketmapResponse{Status: "NOTFOUND"}
	}

	return &SocketmapResponse{Status: "OK", Data: "1"}
}

// handleMailbox processes mailbox lookup requests
func (s *LookupServer) handleMailbox(ctx context.Context, key string) *SocketmapResponse {
	exists, err := s.client.GetMailbox(ctx, key)
	if err != nil {
		logger.Error("Error fetching mailbox", zap.String("key", key), zap.Error(err))
		return &SocketmapResponse{Status: "TEMP", Data: "Error fetching mailbox"}
	}

	if !exists {
		return &SocketmapResponse{Status: "NOTFOUND"}
	}

	return &SocketmapResponse{Status: "OK", Data: "1"}
}

// handleSenders processes senders lookup requests
func (s *LookupServer) handleSenders(ctx context.Context, key string) *SocketmapResponse {
	senders, err := s.client.GetSenders(ctx, key)
	if err != nil {
		logger.Error("Error fetching senders", zap.String("key", key), zap.Error(err))
		return &SocketmapResponse{Status: "TEMP", Data: "Error fetching senders"}
	}

	if len(senders) == 0 {
		return &SocketmapResponse{Status: "NOTFOUND"}
	}

	return &SocketmapResponse{Status: "OK", Data: strings.Join(senders, ",")}
}

// writeResponse sends a socketmap response back to the client
func (s *LookupServer) writeResponse(encoder *netstring.Encoder, conn net.Conn, response *SocketmapResponse, startTime time.Time, mapName string) {
	var status string
	switch response.Status {
	case "OK":
		status = "success"
	case "NOTFOUND":
		status = "notfound"
	default:
		status = "error"
	}

	logger.Debug("Writing socketmap response",
		zap.String("response", response.String()),
		zap.String("map", mapName),
		zap.String("status", status))

	// Set write deadline
	_ = conn.SetWriteDeadline(time.Now().Add(WriteTimeout))

	// Encode and send the response
	err := encoder.EncodeString(netstring.NoKey, response.String())
	if err != nil {
		logger.Error("Error writing response",
			zap.String("response", response.String()),
			zap.String("map", mapName),
			zap.String("status", status),
			zap.Error(err))
	}

	// Record metrics
	duration := time.Since(startTime).Seconds()
	requestDurations.With(prometheus.Labels{"handler": mapName, "status": status}).Observe(duration)
	requestsTotal.With(prometheus.Labels{"handler": mapName, "status": status}).Inc()
}
