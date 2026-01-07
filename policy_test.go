package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
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
