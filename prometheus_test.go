package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
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
		s.Equal(`{"status":"ok"}`+"\n", w.Body.String())
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
		s.Equal(`{"status":"ready"}`+"\n", w.Body.String())

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
		s.Equal(`{"error":"connection refused","status":"unavailable"}`+"\n", w.Body.String())

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
		s.Equal(`{"error":"timeout","status":"unavailable"}`+"\n", w.Body.String())
	})
}

func (s *PrometheusTestSuite) TestHealthCheckLoop() {
	s.Run("updates status independent of ready handler", func() {
		healthCheckStatus.Set(0)
		mockClient := &MockUserliService{}
		mockClient.On("GetDomain", mock.Anything, "health-check.invalid").Return(false, nil)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go runHealthCheckLoop(ctx, mockClient, time.Hour)

		s.Eventually(func() bool {
			return testutil.ToFloat64(healthCheckStatus) == 1
		}, time.Second, 10*time.Millisecond)
	})

	s.Run("sets status to 0 when api fails", func() {
		healthCheckStatus.Set(1)
		mockClient := &MockUserliService{}
		mockClient.On("GetDomain", mock.Anything, "health-check.invalid").Return(false, errors.New("api error"))

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go runHealthCheckLoop(ctx, mockClient, time.Hour)

		s.Eventually(func() bool {
			return testutil.ToFloat64(healthCheckStatus) == 0
		}, time.Second, 10*time.Millisecond)
	})

	s.Run("ready handler does not touch status", func() {
		healthCheckStatus.Set(1)
		mockClient := &MockUserliService{}
		mockClient.On("GetDomain", mock.Anything, "health-check.invalid").Return(false, errors.New("api error"))

		w := httptest.NewRecorder()
		readyHandler(mockClient)(w, httptest.NewRequest(http.MethodGet, "/ready", nil))

		s.Equal(http.StatusServiceUnavailable, w.Code)
		s.Equal(1.0, testutil.ToFloat64(healthCheckStatus))
	})
}

func (s *PrometheusTestSuite) TestStartMetricsServer() {
	s.Run("starts and stops gracefully", func() {
		mockClient := &MockUserliService{}
		mockClient.On("GetDomain", mock.Anything, "health-check.invalid").Return(false, nil).Maybe()

		ctx, cancel := context.WithCancel(context.Background())

		// Use a random available port
		listenAddr := "127.0.0.1:0"

		// Start server in goroutine
		serverStarted := make(chan struct{})
		go func() {
			close(serverStarted)
			StartMetricsServer(ctx, listenAddr, mockClient)
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
