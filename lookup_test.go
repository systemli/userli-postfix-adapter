package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/markdingo/netstring"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"go.uber.org/zap"
)

type ServerTestSuite struct {
	suite.Suite
}

// Helper function to encode a netstring request
func (s *ServerTestSuite) encodeNetstring(data string) []byte {
	return []byte(fmt.Sprintf("%d:%s,", len(data), data))
}

// Helper function to read and decode netstring response from connection
func (s *ServerTestSuite) readNetstringResponse(conn net.Conn) (string, error) {
	// Set a reasonable timeout for reading
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))

	decoder := netstring.NewDecoder(conn)
	responseBytes, err := decoder.Decode()
	if err != nil {
		return "", err
	}
	return string(responseBytes), nil
}

func (s *ServerTestSuite) SetupTest() {
	// Disable logging output during tests
	logger = zap.NewNop()
}

func (s *ServerTestSuite) TearDownTest() {
}

// TestStartLookupServer_BasicFunctionality tests basic server startup and shutdown
func (s *ServerTestSuite) TestStartLookupServer_BasicFunctionality() {
	mock := &MockUserliService{}
	server := NewLookupServer(mock)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// Use port 0 to let the OS assign a free port
	addr := "127.0.0.1:0"

	wg.Add(1)
	go StartLookupServer(ctx, &wg, addr, server)

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Cancel context to trigger shutdown
	cancel()

	// Wait for server to shut down
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Server shut down successfully
	case <-time.After(5 * time.Second):
		s.Fail("Server did not shut down within timeout")
	}
}

// TestStartLookupServer_InvalidAddress tests server behavior with invalid address
func (s *ServerTestSuite) TestStartLookupServer_InvalidAddress() {
	mock := &MockUserliService{}
	server := NewLookupServer(mock)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	// Use an invalid address
	addr := "invalid-address:99999"

	wg.Add(1)
	go StartLookupServer(ctx, &wg, addr, server)

	// Wait for the function to return (should return quickly due to error)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Expected - server should exit due to invalid address
	case <-time.After(2 * time.Second):
		s.Fail("Server did not exit within timeout for invalid address")
	}
}

// TestStartLookupServer_ConnectionHandling tests connection acceptance and handling
func (s *ServerTestSuite) TestStartLookupServer_ConnectionHandling() {
	mockService := &MockUserliService{}
	// Mock a successful domain lookup
	mockService.On("GetDomain", mock.Anything, "example.com").Return(true, nil)
	server := NewLookupServer(mockService)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	// Start server on a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	addr := listener.Addr().String()
	listener.Close() // Close so StartLookupServer can bind to it

	wg.Add(1)
	go StartLookupServer(ctx, &wg, addr, server)

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Connect to the server and send a request
	conn, err := net.Dial("tcp", addr)
	s.Require().NoError(err)
	defer conn.Close()

	// Send a netstring-encoded socketmap request
	request := "18:domain example.com,"
	_, err = conn.Write([]byte(request))
	s.Require().NoError(err)

	// Read the response using netstring decoder
	decodedResponse, err := s.readNetstringResponse(conn)
	s.Require().NoError(err)
	s.Contains(decodedResponse, "OK 1", "Expected successful domain lookup response")

	mockService.AssertExpectations(s.T())
}

// TestStartLookupServer_GracefulShutdown tests graceful shutdown with active connections
func (s *ServerTestSuite) TestStartLookupServer_GracefulShutdown() {
	mockService := &MockUserliService{}
	server := NewLookupServer(mockService)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// Start server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	addr := listener.Addr().String()
	listener.Close()

	wg.Add(1)
	go StartLookupServer(ctx, &wg, addr, server)

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Connect multiple clients
	var clients []net.Conn
	for i := 0; i < 3; i++ {
		conn, err := net.Dial("tcp", addr)
		s.Require().NoError(err)
		clients = append(clients, conn)
	}

	// Trigger shutdown
	cancel()

	// Close client connections
	for _, conn := range clients {
		conn.Close()
	}

	// Wait for server to shut down
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Server shut down successfully
	case <-time.After(5 * time.Second):
		s.Fail("Server did not shut down gracefully within timeout")
	}
}

// TestHandleLookupConnection tests the connection handler function
func (s *ServerTestSuite) TestHandleLookupConnection() {
	mockService := &MockUserliService{}
	mockService.On("GetDomain", mock.Anything, "example.com").Return(true, nil)
	server := NewLookupServer(mockService)

	// Create a pipe to simulate a connection
	serverConn, client := net.Pipe()
	defer serverConn.Close()
	defer client.Close()

	// Start the connection handler
	go server.HandleConnection(context.Background(), serverConn)

	// Send a request from the client side
	request := "18:domain example.com,"
	_, err := client.Write([]byte(request))
	s.Require().NoError(err)

	// Read the response using netstring decoder
	decodedResponse, err := s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(decodedResponse, "OK 1")

	mockService.AssertExpectations(s.T())
}

// TestStartLookupServer_ConnectionPoolLimit tests connection pool limits
func (s *ServerTestSuite) TestStartLookupServer_ConnectionPoolLimit() {
	mockService := &MockUserliService{}
	server := NewLookupServer(mockService)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup

	// Start server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	addr := listener.Addr().String()
	listener.Close()

	wg.Add(1)
	go StartLookupServer(ctx, &wg, addr, server)

	// Give the server a moment to start
	time.Sleep(100 * time.Millisecond)

	// Create multiple connections (but don't exceed the limit for this test)
	var connections []net.Conn
	maxConnections := 10 // Use a small number for testing

	for i := 0; i < maxConnections; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			// Some connections might fail if we hit the limit, which is expected
			break
		}
		connections = append(connections, conn)
	}

	s.True(len(connections) > 0, "Should be able to establish at least some connections")

	// Clean up connections
	for _, conn := range connections {
		conn.Close()
	}

	// Give connections time to close
	time.Sleep(200 * time.Millisecond)
}

// TestHandleLookupConnection_MultipleRequests tests handling multiple requests on same connection
func (s *ServerTestSuite) TestHandleLookupConnection_MultipleRequests() {
	mockService := &MockUserliService{}
	mockService.On("GetDomain", mock.Anything, "example.com").Return(true, nil)
	mockService.On("GetDomain", mock.Anything, "example.org").Return(false, nil)
	server := NewLookupServer(mockService)

	serverConn, client := net.Pipe()
	defer serverConn.Close()
	defer client.Close()

	// Start the connection handler
	go server.HandleConnection(context.Background(), serverConn)

	// Send first request
	request1 := "18:domain example.com,"
	_, err := client.Write([]byte(request1))
	s.Require().NoError(err)

	// Read first response
	decodedResponse1, err := s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(decodedResponse1, "OK 1")

	// Send second request
	request2 := "18:domain example.org,"
	_, err = client.Write([]byte(request2))
	s.Require().NoError(err)

	// Read second response
	decodedResponse2, err := s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(decodedResponse2, "NOTFOUND")

	mockService.AssertExpectations(s.T())
}

// TestHandleLookupConnection_AliasLookup tests alias lookup handler
func (s *ServerTestSuite) TestHandleLookupConnection_AliasLookup() {
	mockService := &MockUserliService{}
	mockService.On("GetAliases", mock.Anything, "alias@example.com").Return([]string{"user1@example.com", "user2@example.com"}, nil)
	mockService.On("GetAliases", mock.Anything, "unknown@example.com").Return([]string{}, nil)
	mockService.On("GetAliases", mock.Anything, "error@example.com").Return([]string(nil), io.ErrUnexpectedEOF)
	server := NewLookupServer(mockService)

	serverConn, client := net.Pipe()
	defer serverConn.Close()
	defer client.Close()

	go server.HandleConnection(context.Background(), serverConn)

	// Test successful alias lookup
	_, err := client.Write(s.encodeNetstring("alias alias@example.com"))
	s.Require().NoError(err)
	response, err := s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(response, "OK user1@example.com,user2@example.com")

	// Test not found
	_, err = client.Write(s.encodeNetstring("alias unknown@example.com"))
	s.Require().NoError(err)
	response, err = s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(response, "NOTFOUND")

	// Test error case
	_, err = client.Write(s.encodeNetstring("alias error@example.com"))
	s.Require().NoError(err)
	response, err = s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(response, "TEMP")

	mockService.AssertExpectations(s.T())
}

// TestHandleLookupConnection_MailboxLookup tests mailbox lookup handler
func (s *ServerTestSuite) TestHandleLookupConnection_MailboxLookup() {
	mockService := &MockUserliService{}
	mockService.On("GetMailbox", mock.Anything, "user@example.com").Return(true, nil)
	mockService.On("GetMailbox", mock.Anything, "unknown@example.com").Return(false, nil)
	mockService.On("GetMailbox", mock.Anything, "error@example.com").Return(false, io.ErrUnexpectedEOF)
	server := NewLookupServer(mockService)

	serverConn, client := net.Pipe()
	defer serverConn.Close()
	defer client.Close()

	go server.HandleConnection(context.Background(), serverConn)

	// Test successful mailbox lookup
	_, err := client.Write(s.encodeNetstring("mailbox user@example.com"))
	s.Require().NoError(err)
	response, err := s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(response, "OK 1")

	// Test not found
	_, err = client.Write(s.encodeNetstring("mailbox unknown@example.com"))
	s.Require().NoError(err)
	response, err = s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(response, "NOTFOUND")

	// Test error case
	_, err = client.Write(s.encodeNetstring("mailbox error@example.com"))
	s.Require().NoError(err)
	response, err = s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(response, "TEMP")

	mockService.AssertExpectations(s.T())
}

// TestHandleLookupConnection_SendersLookup tests senders lookup handler
func (s *ServerTestSuite) TestHandleLookupConnection_SendersLookup() {
	mockService := &MockUserliService{}
	mockService.On("GetSenders", mock.Anything, "user@example.com").Return([]string{"alias1@example.com", "alias2@example.com"}, nil)
	mockService.On("GetSenders", mock.Anything, "unknown@example.com").Return([]string{}, nil)
	mockService.On("GetSenders", mock.Anything, "error@example.com").Return([]string(nil), io.ErrUnexpectedEOF)
	server := NewLookupServer(mockService)

	serverConn, client := net.Pipe()
	defer serverConn.Close()
	defer client.Close()

	go server.HandleConnection(context.Background(), serverConn)

	// Test successful senders lookup
	_, err := client.Write(s.encodeNetstring("senders user@example.com"))
	s.Require().NoError(err)
	response, err := s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(response, "OK alias1@example.com,alias2@example.com")

	// Test not found
	_, err = client.Write(s.encodeNetstring("senders unknown@example.com"))
	s.Require().NoError(err)
	response, err = s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(response, "NOTFOUND")

	// Test error case
	_, err = client.Write(s.encodeNetstring("senders error@example.com"))
	s.Require().NoError(err)
	response, err = s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(response, "TEMP")

	mockService.AssertExpectations(s.T())
}

// TestHandleLookupConnection_DomainError tests domain lookup error handling
func (s *ServerTestSuite) TestHandleLookupConnection_DomainError() {
	mockService := &MockUserliService{}
	mockService.On("GetDomain", mock.Anything, "error.com").Return(false, io.ErrUnexpectedEOF)
	server := NewLookupServer(mockService)

	serverConn, client := net.Pipe()
	defer serverConn.Close()
	defer client.Close()

	go server.HandleConnection(context.Background(), serverConn)

	_, err := client.Write(s.encodeNetstring("domain error.com"))
	s.Require().NoError(err)
	response, err := s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(response, "TEMP")

	mockService.AssertExpectations(s.T())
}

// TestHandleLookupConnection_UnknownMap tests unknown map name handling
func (s *ServerTestSuite) TestHandleLookupConnection_UnknownMap() {
	mockService := &MockUserliService{}
	server := NewLookupServer(mockService)

	serverConn, client := net.Pipe()
	defer serverConn.Close()
	defer client.Close()

	go server.HandleConnection(context.Background(), serverConn)

	_, err := client.Write(s.encodeNetstring("unknownmap somekey"))
	s.Require().NoError(err)
	response, err := s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(response, "PERM")
	s.Contains(response, "Unknown map name")
}

// TestHandleLookupConnection_InvalidFormat tests invalid request format
func (s *ServerTestSuite) TestHandleLookupConnection_InvalidFormat() {
	mockService := &MockUserliService{}
	server := NewLookupServer(mockService)

	serverConn, client := net.Pipe()
	defer serverConn.Close()
	defer client.Close()

	go server.HandleConnection(context.Background(), serverConn)

	// Send invalid format (no space separator)
	_, err := client.Write(s.encodeNetstring("invalidrequest"))
	s.Require().NoError(err)
	response, err := s.readNetstringResponse(client)
	s.Require().NoError(err)
	s.Contains(response, "PERM")
	s.Contains(response, "Invalid request format")
}

// TestHandleLookupConnection_ContextCancelled tests context cancellation
func (s *ServerTestSuite) TestHandleLookupConnection_ContextCancelled() {
	mockService := &MockUserliService{}
	server := NewLookupServer(mockService)

	serverConn, client := net.Pipe()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		server.HandleConnection(ctx, serverConn)
		close(done)
	}()

	// Cancel context and close connection to unblock the read
	cancel()
	client.Close()
	serverConn.Close()

	// Handler should exit
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		s.Fail("Handler did not exit after context cancellation")
	}
}

// TestSocketmapResponse_String tests the SocketmapResponse.String method
func (s *ServerTestSuite) TestSocketmapResponse_String() {
	// Test with data
	resp := &SocketmapResponse{Status: "OK", Data: "test data"}
	s.Equal("OK test data", resp.String())

	// Test without data
	resp = &SocketmapResponse{Status: "NOTFOUND", Data: ""}
	s.Equal("NOTFOUND", resp.String())
}

func TestServer(t *testing.T) {
	suite.Run(t, new(ServerTestSuite))
}
