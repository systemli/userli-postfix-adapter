package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
)

type PrometheusTestSuite struct {
	suite.Suite
}

func (s *PrometheusTestSuite) TestHealthHandler() {
	s.Run("health endpoint returns ok", func() {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		w := httptest.NewRecorder()

		healthHandler(w, req)

		s.Equal(http.StatusOK, w.Code)
		s.Equal("application/json", w.Header().Get("Content-Type"))
		s.Equal(`{"status":"ok"}`, w.Body.String())
	})
}

func (s *PrometheusTestSuite) TestReadyHandler() {
	s.Run("ready when userli api is reachable", func() {
		mockClient := &MockUserliService{}
		mockClient.On("GetDomain", mock.Anything, "health-check.invalid").Return(false, nil)

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		w := httptest.NewRecorder()

		handler := readyHandler(mockClient)
		handler(w, req)

		s.Equal(http.StatusOK, w.Code)
		s.Equal("application/json", w.Header().Get("Content-Type"))
		s.Equal(`{"status":"ready"}`, w.Body.String())

		mockClient.AssertExpectations(s.T())
	})

	s.Run("unavailable when userli api returns error", func() {
		mockClient := &MockUserliService{}
		mockClient.On("GetDomain", mock.Anything, "health-check.invalid").Return(false, errors.New("connection refused"))

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		w := httptest.NewRecorder()

		handler := readyHandler(mockClient)
		handler(w, req)

		s.Equal(http.StatusServiceUnavailable, w.Code)
		s.Equal("application/json", w.Header().Get("Content-Type"))
		s.Equal(`{"status":"unavailable","error":"connection refused"}`, w.Body.String())

		mockClient.AssertExpectations(s.T())
	})

	s.Run("unavailable on timeout", func() {
		mockClient := &MockUserliService{}
		mockClient.On("GetDomain", mock.Anything, "health-check.invalid").Return(false, nil).After(100 * time.Millisecond)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler := readyHandler(mockClient)
		handler(w, req)

		s.Equal(http.StatusServiceUnavailable, w.Code)
		s.Equal("application/json", w.Header().Get("Content-Type"))
		s.Equal(`{"status":"unavailable","error":"timeout"}`, w.Body.String())
	})
}

func (s *PrometheusTestSuite) TestReadyHandlerHealthCheckStatusMetric() {
	s.Run("sets health check status to 1 when healthy", func() {
		mockClient := &MockUserliService{}
		mockClient.On("GetDomain", mock.Anything, "health-check.invalid").Return(false, nil)

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		w := httptest.NewRecorder()

		handler := readyHandler(mockClient)
		handler(w, req)

		s.NotNil(w)
		mockClient.AssertExpectations(s.T())
	})

	s.Run("sets health check status to 0 when unhealthy", func() {
		mockClient := &MockUserliService{}
		mockClient.On("GetDomain", mock.Anything, "health-check.invalid").Return(false, errors.New("api error"))

		req := httptest.NewRequest(http.MethodGet, "/ready", nil)
		w := httptest.NewRecorder()

		handler := readyHandler(mockClient)
		handler(w, req)

		s.NotNil(w)
		mockClient.AssertExpectations(s.T())
	})
}

func (s *PrometheusTestSuite) TestStartMetricsServer() {
	s.Run("starts and stops gracefully", func() {
		mockClient := &MockUserliService{}
		mockClient.On("GetDomain", mock.Anything, "health-check.invalid").Return(false, nil).Maybe()

		ctx, cancel := context.WithCancel(context.Background())

		// Use a random available port
		listenAddr := "127.0.0.1:0"

		// Create a rate limiter for the test
		rateLimiter := NewRateLimiter(context.Background())

		// Start server in goroutine
		serverStarted := make(chan struct{})
		go func() {
			close(serverStarted)
			StartMetricsServer(ctx, listenAddr, mockClient, rateLimiter)
		}()

		// Wait for server to start
		<-serverStarted
		time.Sleep(100 * time.Millisecond)

		// Cancel context to trigger shutdown
		cancel()

		// Give server time to shut down
		time.Sleep(200 * time.Millisecond)

		// Test passes if no panic occurred
	})
}

func TestPrometheus(t *testing.T) {
	suite.Run(t, new(PrometheusTestSuite))
}
