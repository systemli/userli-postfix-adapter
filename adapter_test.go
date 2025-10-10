package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
)

// Mock connection for testing
type MockConn struct {
	readBuffer  *bytes.Buffer
	writeBuffer *bytes.Buffer
	closed      bool
}

func NewMockConn(input string) *MockConn {
	return &MockConn{
		readBuffer:  bytes.NewBufferString(input),
		writeBuffer: &bytes.Buffer{},
	}
}

func (m *MockConn) Read(b []byte) (n int, err error) {
	return m.readBuffer.Read(b)
}

func (m *MockConn) Write(b []byte) (n int, err error) {
	return m.writeBuffer.Write(b)
}

func (m *MockConn) Close() error {
	m.closed = true
	return nil
}

func (m *MockConn) LocalAddr() net.Addr                { return nil }
func (m *MockConn) RemoteAddr() net.Addr               { return nil }
func (m *MockConn) SetDeadline(t time.Time) error      { return nil }
func (m *MockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *MockConn) SetWriteDeadline(t time.Time) error { return nil }

func (m *MockConn) GetWritten() string {
	return m.writeBuffer.String()
}

func TestSocketmapResponse_String(t *testing.T) {
	tests := []struct {
		name     string
		response SocketmapResponse
		expected string
	}{
		{
			name:     "OK with data",
			response: SocketmapResponse{Status: "OK", Data: "test@example.com"},
			expected: "OK test@example.com",
		},
		{
			name:     "NOTFOUND without data",
			response: SocketmapResponse{Status: "NOTFOUND", Data: ""},
			expected: "NOTFOUND",
		},
		{
			name:     "TEMP with error message",
			response: SocketmapResponse{Status: "TEMP", Data: "Service unavailable"},
			expected: "TEMP Service unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.response.String()
			if result != tt.expected {
				t.Errorf("String() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestSocketmapAdapter_handleAlias(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		setup    func(*MockUserliService)
		expected SocketmapResponse
	}{
		{
			name: "existing alias",
			key:  "alias@example.com",
			setup: func(m *MockUserliService) {
				m.On("GetAliases", mock.Anything, "alias@example.com").Return([]string{"user1@example.com", "user2@example.com"}, nil)
			},
			expected: SocketmapResponse{Status: "OK", Data: "user1@example.com,user2@example.com"},
		},
		{
			name: "non-existing alias",
			key:  "nonexistent@example.com",
			setup: func(m *MockUserliService) {
				m.On("GetAliases", mock.Anything, "nonexistent@example.com").Return([]string{}, nil)
			},
			expected: SocketmapResponse{Status: "NOTFOUND"},
		},
		{
			name: "service error",
			key:  "error@example.com",
			setup: func(m *MockUserliService) {
				m.On("GetAliases", mock.Anything, "error@example.com").Return([]string{}, errors.New("service error"))
			},
			expected: SocketmapResponse{Status: "TEMP", Data: "Error fetching aliases"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockUserliService{}
			tt.setup(mock)
			adapter := NewSocketmapAdapter(mock)

			ctx := context.Background()
			result := adapter.handleAlias(ctx, tt.key)

			if result.Status != tt.expected.Status {
				t.Errorf("handleAlias() status = %q, want %q", result.Status, tt.expected.Status)
			}
			if result.Data != tt.expected.Data {
				t.Errorf("handleAlias() data = %q, want %q", result.Data, tt.expected.Data)
			}
			mock.AssertExpectations(t)
		})
	}
}

func TestSocketmapAdapter_handleDomain(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		setup    func(*MockUserliService)
		expected SocketmapResponse
	}{
		{
			name: "existing domain",
			key:  "example.com",
			setup: func(m *MockUserliService) {
				m.On("GetDomain", mock.Anything, "example.com").Return(true, nil)
			},
			expected: SocketmapResponse{Status: "OK", Data: "1"},
		},
		{
			name: "non-existing domain",
			key:  "nonexistent.com",
			setup: func(m *MockUserliService) {
				m.On("GetDomain", mock.Anything, "nonexistent.com").Return(false, nil)
			},
			expected: SocketmapResponse{Status: "NOTFOUND"},
		},
		{
			name: "service error",
			key:  "error.com",
			setup: func(m *MockUserliService) {
				m.On("GetDomain", mock.Anything, "error.com").Return(false, errors.New("service error"))
			},
			expected: SocketmapResponse{Status: "TEMP", Data: "Error fetching domain"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockUserliService{}
			tt.setup(mock)
			adapter := NewSocketmapAdapter(mock)

			ctx := context.Background()
			result := adapter.handleDomain(ctx, tt.key)

			if result.Status != tt.expected.Status {
				t.Errorf("handleDomain() status = %q, want %q", result.Status, tt.expected.Status)
			}
			if result.Data != tt.expected.Data {
				t.Errorf("handleDomain() data = %q, want %q", result.Data, tt.expected.Data)
			}
			mock.AssertExpectations(t)
		})
	}
}

func TestSocketmapAdapter_handleMailbox(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		setup    func(*MockUserliService)
		expected SocketmapResponse
	}{
		{
			name: "existing mailbox",
			key:  "user@example.com",
			setup: func(m *MockUserliService) {
				m.On("GetMailbox", mock.Anything, "user@example.com").Return(true, nil)
			},
			expected: SocketmapResponse{Status: "OK", Data: "1"},
		},
		{
			name: "non-existing mailbox",
			key:  "nonexistent@example.com",
			setup: func(m *MockUserliService) {
				m.On("GetMailbox", mock.Anything, "nonexistent@example.com").Return(false, nil)
			},
			expected: SocketmapResponse{Status: "NOTFOUND"},
		},
		{
			name: "service error",
			key:  "error@example.com",
			setup: func(m *MockUserliService) {
				m.On("GetMailbox", mock.Anything, "error@example.com").Return(false, errors.New("service error"))
			},
			expected: SocketmapResponse{Status: "TEMP", Data: "Error fetching mailbox"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockUserliService{}
			tt.setup(mock)
			adapter := NewSocketmapAdapter(mock)

			ctx := context.Background()
			result := adapter.handleMailbox(ctx, tt.key)

			if result.Status != tt.expected.Status {
				t.Errorf("handleMailbox() status = %q, want %q", result.Status, tt.expected.Status)
			}
			if result.Data != tt.expected.Data {
				t.Errorf("handleMailbox() data = %q, want %q", result.Data, tt.expected.Data)
			}
			mock.AssertExpectations(t)
		})
	}
}

func TestSocketmapAdapter_handleSenders(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		setup    func(*MockUserliService)
		expected SocketmapResponse
	}{
		{
			name: "existing senders",
			key:  "user@example.com",
			setup: func(m *MockUserliService) {
				m.On("GetSenders", mock.Anything, "user@example.com").Return([]string{"sender1@example.com", "sender2@example.com"}, nil)
			},
			expected: SocketmapResponse{Status: "OK", Data: "sender1@example.com,sender2@example.com"},
		},
		{
			name: "no senders",
			key:  "nosenders@example.com",
			setup: func(m *MockUserliService) {
				m.On("GetSenders", mock.Anything, "nosenders@example.com").Return([]string{}, nil)
			},
			expected: SocketmapResponse{Status: "NOTFOUND"},
		},
		{
			name: "service error",
			key:  "error@example.com",
			setup: func(m *MockUserliService) {
				m.On("GetSenders", mock.Anything, "error@example.com").Return([]string{}, errors.New("service error"))
			},
			expected: SocketmapResponse{Status: "TEMP", Data: "Error fetching senders"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockUserliService{}
			tt.setup(mock)
			adapter := NewSocketmapAdapter(mock)

			ctx := context.Background()
			result := adapter.handleSenders(ctx, tt.key)

			if result.Status != tt.expected.Status {
				t.Errorf("handleSenders() status = %q, want %q", result.Status, tt.expected.Status)
			}
			if result.Data != tt.expected.Data {
				t.Errorf("handleSenders() data = %q, want %q", result.Data, tt.expected.Data)
			}
			mock.AssertExpectations(t)
		})
	}
}

func TestSocketmapAdapter_HandleConnection(t *testing.T) {
	tests := []struct {
		name           string
		requests       []string
		setup          func(*MockUserliService)
		expectedCount  int
		expectedOutput []string
	}{
		{
			name:     "single alias request",
			requests: []string{"22:alias test@example.com,"},
			setup: func(m *MockUserliService) {
				m.On("GetAliases", mock.Anything, "test@example.com").Return([]string{"dest@example.com"}, nil)
			},
			expectedCount:  1,
			expectedOutput: []string{"19:OK dest@example.com,"},
		},
		{
			name:     "single domain request",
			requests: []string{"18:domain example.com,"},
			setup: func(m *MockUserliService) {
				m.On("GetDomain", mock.Anything, "example.com").Return(true, nil)
			},
			expectedCount:  1,
			expectedOutput: []string{"4:OK 1,"},
		},
		{
			name:           "invalid request format",
			requests:       []string{"10:invalidreq,"},
			setup:          func(m *MockUserliService) {},
			expectedCount:  1,
			expectedOutput: []string{"27:PERM Invalid request format,"},
		},
		{
			name:           "unknown map name",
			requests:       []string{"24:unknown test@example.com,"},
			setup:          func(m *MockUserliService) {},
			expectedCount:  1,
			expectedOutput: []string{"21:PERM Unknown map name,"},
		},
		{
			name: "multiple requests",
			requests: []string{
				"22:alias test@example.com,",
				"18:domain example.com,",
			},
			setup: func(m *MockUserliService) {
				m.On("GetAliases", mock.Anything, "test@example.com").Return([]string{"dest@example.com"}, nil)
				m.On("GetDomain", mock.Anything, "example.com").Return(true, nil)
			},
			expectedCount: 2,
			expectedOutput: []string{
				"19:OK dest@example.com,",
				"4:OK 1,",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockUserliService{}
			tt.setup(mock)
			adapter := NewSocketmapAdapter(mock)

			// Create input with all requests
			input := strings.Join(tt.requests, "")
			conn := NewMockConn(input)

			// Handle the connection
			adapter.HandleConnection(conn)

			// Check if connection was closed
			if !conn.closed {
				t.Error("Connection should be closed after handling")
			}

			// Parse the output
			output := conn.GetWritten()

			// For single response tests, check exact match
			if len(tt.expectedOutput) == 1 && len(tt.requests) == 1 {
				if output != tt.expectedOutput[0] {
					t.Errorf("HandleConnection() output = %q, want %q", output, tt.expectedOutput[0])
				}
			} else {
				// For multiple responses, check that all expected outputs are present
				for _, expected := range tt.expectedOutput {
					if !strings.Contains(output, expected) {
						t.Errorf("HandleConnection() output missing expected response %q in %q", expected, output)
					}
				}
			}

			// Only verify mock expectations for tests that should call the mock
			if strings.Contains(tt.name, "alias") || strings.Contains(tt.name, "domain") || strings.Contains(tt.name, "multiple") {
				mock.AssertExpectations(t)
			}
		})
	}
}

func TestSocketmapAdapter_HandleConnection_InvalidNetstring(t *testing.T) {
	mock := &MockUserliService{}
	adapter := NewSocketmapAdapter(mock)

	// Invalid netstring (missing colon)
	conn := NewMockConn("5hello,")

	// Handle the connection - should exit gracefully on decode error
	adapter.HandleConnection(conn)

	// Check if connection was closed
	if !conn.closed {
		t.Error("Connection should be closed after handling invalid netstring")
	}

	// Should have no output since the decode failed
	output := conn.GetWritten()
	if output != "" {
		t.Errorf("HandleConnection() with invalid netstring should produce no output, got %q", output)
	}
}
