package main

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	log "github.com/sirupsen/logrus"
)

var (
	requestDurations = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "userli_postfix_adapter_request_duration_seconds",
		Help:    "Duration of requests to userli",
		Buckets: prometheus.ExponentialBuckets(0.1, 1.5, 5.0),
	}, []string{"handler", "status"})
)

// StartMetricsServer starts a new HTTP server for prometheus metrics.
func StartMetricsServer(ctx context.Context, listenAddr string) {
	registry := prometheus.NewRegistry()

	registry.MustRegister(
		collectors.NewGoCollector(),
		requestDurations,
	)

	http.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))

	log.Info("Metrics server started on ", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, nil))
}
