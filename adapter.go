package main

import (
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/markdingo/netstring"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
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

// SocketmapAdapter handles socketmap protocol requests
type SocketmapAdapter struct {
	client UserliService
}

// NewSocketmapAdapter creates a new SocketmapAdapter with the given UserliService
func NewSocketmapAdapter(client UserliService) *SocketmapAdapter {
	return &SocketmapAdapter{client: client}
}

// HandleConnection processes a single socketmap connection
// Supports persistent connections with multiple requests
func (s *SocketmapAdapter) HandleConnection(conn net.Conn) {
	defer func() {
		if err := conn.Close(); err != nil {
			log.WithError(err).Error("Error closing connection")
		}
	}()

	decoder := netstring.NewDecoder(conn)
	encoder := netstring.NewEncoder(conn)

	for {
		// Set read deadline for each request
		_ = conn.SetReadDeadline(time.Now().Add(ReadTimeout))

		// Read the request netstring
		requestBytes, err := decoder.Decode()
		if err != nil {
			// Check if this is a normal connection closure (EOF) or an actual error
			if err == io.EOF {
				log.Debug("Client closed connection")
			} else {
				log.WithError(err).Debug("Failed to decode request")
			}
			return
		}
		request := string(requestBytes)

		now := time.Now()

		// Parse the request: "name key"
		parts := strings.SplitN(strings.TrimSpace(request), " ", 2)
		if len(parts) != 2 {
			log.WithField("request", request).Error("Invalid request format")
			response := &SocketmapResponse{Status: "PERM", Data: "Invalid request format"}
			s.writeResponse(encoder, conn, response, now, "invalid")
			continue
		}

		mapName := parts[0]
		key := parts[1]

		log.WithFields(log.Fields{
			"map": mapName,
			"key": key,
		}).Debug("Processing socketmap request")

		// Route to appropriate handler based on map name
		var response *SocketmapResponse
		switch mapName {
		case "alias":
			response = s.handleAlias(key)
		case "domain":
			response = s.handleDomain(key)
		case "mailbox":
			response = s.handleMailbox(key)
		case "senders":
			response = s.handleSenders(key)
		default:
			log.WithField("map", mapName).Error("Unknown map name")
			response = &SocketmapResponse{Status: "PERM", Data: "Unknown map name"}
		}

		s.writeResponse(encoder, conn, response, now, mapName)
	}
}

// handleAlias processes alias lookup requests
func (s *SocketmapAdapter) handleAlias(key string) *SocketmapResponse {
	aliases, err := s.client.GetAliases(key)
	if err != nil {
		log.WithError(err).WithField("key", key).Error("Error fetching aliases")
		return &SocketmapResponse{Status: "TEMP", Data: "Error fetching aliases"}
	}

	if len(aliases) == 0 {
		return &SocketmapResponse{Status: "NOTFOUND"}
	}

	return &SocketmapResponse{Status: "OK", Data: strings.Join(aliases, ",")}
}

// handleDomain processes domain lookup requests
func (s *SocketmapAdapter) handleDomain(key string) *SocketmapResponse {
	exists, err := s.client.GetDomain(key)
	if err != nil {
		log.WithError(err).WithField("key", key).Error("Error fetching domain")
		return &SocketmapResponse{Status: "TEMP", Data: "Error fetching domain"}
	}

	if !exists {
		return &SocketmapResponse{Status: "NOTFOUND"}
	}

	return &SocketmapResponse{Status: "OK", Data: "1"}
}

// handleMailbox processes mailbox lookup requests
func (s *SocketmapAdapter) handleMailbox(key string) *SocketmapResponse {
	exists, err := s.client.GetMailbox(key)
	if err != nil {
		log.WithError(err).WithField("key", key).Error("Error fetching mailbox")
		return &SocketmapResponse{Status: "TEMP", Data: "Error fetching mailbox"}
	}

	if !exists {
		return &SocketmapResponse{Status: "NOTFOUND"}
	}

	return &SocketmapResponse{Status: "OK", Data: "1"}
}

// handleSenders processes senders lookup requests
func (s *SocketmapAdapter) handleSenders(key string) *SocketmapResponse {
	senders, err := s.client.GetSenders(key)
	if err != nil {
		log.WithError(err).WithField("key", key).Error("Error fetching senders")
		return &SocketmapResponse{Status: "TEMP", Data: "Error fetching senders"}
	}

	if len(senders) == 0 {
		return &SocketmapResponse{Status: "NOTFOUND"}
	}

	return &SocketmapResponse{Status: "OK", Data: strings.Join(senders, ",")}
}

// writeResponse sends a socketmap response back to the client
func (s *SocketmapAdapter) writeResponse(encoder *netstring.Encoder, conn net.Conn, response *SocketmapResponse, startTime time.Time, mapName string) {
	var status string
	var result string
	switch response.Status {
	case "OK":
		status = "success"
		result = "ok"
	case "NOTFOUND":
		status = "notfound"
		result = "notfound"
	default:
		status = "error"
		result = "error"
	}

	log.WithFields(log.Fields{
		"response": response.String(),
		"map":      mapName,
		"status":   status,
	}).Debug("Writing socketmap response")

	// Set write deadline
	_ = conn.SetWriteDeadline(time.Now().Add(WriteTimeout))

	// Encode and send the response
	err := encoder.EncodeString(netstring.NoKey, response.String())
	if err != nil {
		log.WithError(err).WithFields(log.Fields{
			"response": response.String(),
			"map":      mapName,
			"status":   status,
		}).Error("Error writing response")
	}

	// Record metrics
	duration := time.Since(startTime).Seconds()
	requestDurations.With(prometheus.Labels{"handler": mapName, "status": status}).Observe(duration)
	requestsTotal.With(prometheus.Labels{"handler": mapName, "status": status, "result": result}).Inc()
}
