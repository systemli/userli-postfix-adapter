package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	log "github.com/sirupsen/logrus"
)

var (
	// Request duration histogram
	requestDurations = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "userli_postfix_adapter_request_duration_seconds",
		Help:    "Duration of socketmap requests",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 10), // 1ms to ~1s
	}, []string{"handler", "status"})

	// Request counter
	requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "userli_postfix_adapter_requests_total",
		Help: "Total number of socketmap requests",
	}, []string{"handler", "status", "result"})

	// Active connections gauge
	activeConnections = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "userli_postfix_adapter_active_connections",
		Help: "Number of currently active socketmap connections",
	})

	// Connection pool usage gauge
	connectionPoolUsage = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "userli_postfix_adapter_connection_pool_usage",
		Help: "Current usage of the connection pool (0-500)",
	})

	// HTTP client request duration
	httpClientDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "userli_postfix_adapter_http_client_duration_seconds",
		Help:    "Duration of HTTP requests to Userli API",
		Buckets: prometheus.ExponentialBuckets(0.01, 2, 10), // 10ms to ~10s
	}, []string{"endpoint", "status_code"})

	// HTTP client request counter
	httpClientRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "userli_postfix_adapter_http_client_requests_total",
		Help: "Total number of HTTP requests to Userli API",
	}, []string{"endpoint", "status_code"})

	// Health check status
	healthCheckStatus = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "userli_postfix_adapter_health_check_status",
		Help: "Health check status (1 = healthy, 0 = unhealthy)",
	})
)

// StartMetricsServer starts a new HTTP server for prometheus metrics and health checks.
func StartMetricsServer(ctx context.Context, listenAddr string, userliClient UserliService) {
	registry := prometheus.NewRegistry()

	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		requestDurations,
		requestsTotal,
		activeConnections,
		connectionPoolUsage,
		httpClientDuration,
		httpClientRequestsTotal,
		healthCheckStatus,
	)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", readyHandler(userliClient))

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown handler
	go func() {
		<-ctx.Done()
		log.Info("Shutting down metrics server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.WithError(err).Error("Error shutting down metrics server")
		}
		log.Info("Metrics server stopped")
	}()

	log.WithField("addr", listenAddr).Info("Metrics server started")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.WithError(err).Fatal("Metrics server failed")
	}
}

// healthHandler handles liveness probe requests
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok"}`)
}

// readyHandler handles readiness probe requests (checks Userli API connectivity)
func readyHandler(userliClient UserliService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Try to query a test domain to verify Userli API is reachable
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		// Use a channel to make the blocking call cancellable
		resultChan := make(chan error, 1)
		go func() {
			_, err := userliClient.GetDomain("health-check.invalid")
			resultChan <- err
		}()

		select {
		case err := <-resultChan:
			if err != nil {
				healthCheckStatus.Set(0)
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintf(w, `{"status":"unavailable","error":"%s"}`, err.Error())
				log.WithError(err).Warn("Readiness check failed")
				return
			}
			healthCheckStatus.Set(1)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"status":"ready"}`)
		case <-ctx.Done():
			healthCheckStatus.Set(0)
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, `{"status":"unavailable","error":"timeout"}`)
			log.Warn("Readiness check timeout")
		}
	}
}
