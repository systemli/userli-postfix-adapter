package main

import (
	"fmt"
	"os"
)

// Config is the configuration for the application.
type Config struct {
	// UserliToken is the token for the userli service.
	UserliToken string

	// UserliBaseURL is the base URL for the userli service.
	UserliBaseURL string

	// PostfixRecipientDelimiter is the recipient delimiter used by Postfix.
	PostfixRecipientDelimiter string

	// SocketmapListenAddr is the address to listen for socketmap requests.
	SocketmapListenAddr string

	// MetricsListenAddr is the address to listen for metrics requests.
	MetricsListenAddr string
}

// NewConfig creates a new Config with default values.
func NewConfig() (*Config, error) {
	userliBaseURL := os.Getenv("USERLI_BASE_URL")
	if userliBaseURL == "" {
		userliBaseURL = "http://localhost:8000"
	}

	userliToken := os.Getenv("USERLI_TOKEN")
	if userliToken == "" {
		return nil, fmt.Errorf("USERLI_TOKEN is required")
	}

	postfixRecipientDelimiter := os.Getenv("POSTFIX_RECIPIENT_DELIMITER")

	socketmapListenAddr := os.Getenv("SOCKETMAP_LISTEN_ADDR")
	if socketmapListenAddr == "" {
		socketmapListenAddr = ":10001"
	}

	metricsListenAddr := os.Getenv("METRICS_LISTEN_ADDR")
	if metricsListenAddr == "" {
		metricsListenAddr = ":10002"
	}

	return &Config{
		UserliBaseURL:             userliBaseURL,
		UserliToken:               userliToken,
		PostfixRecipientDelimiter: postfixRecipientDelimiter,
		SocketmapListenAddr:       socketmapListenAddr,
		MetricsListenAddr:         metricsListenAddr,
	}, nil
}
