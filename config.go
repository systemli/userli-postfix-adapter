package main

import (
	"os"

	log "github.com/sirupsen/logrus"
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
func NewConfig() *Config {
	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	logFormat := os.Getenv("LOG_FORMAT")
	if logFormat == "" {
		logFormat = "text"
	}

	level, err := log.ParseLevel(logLevel)
	if err != nil {
		log.WithError(err).Fatal("Failed to parse log level")
	}
	log.SetLevel(level)

	if logFormat == "json" {
		log.SetFormatter(&log.JSONFormatter{})
	} else {
		log.SetFormatter(&log.TextFormatter{})
	}

	userliBaseURL := os.Getenv("USERLI_BASE_URL")
	if userliBaseURL == "" {
		userliBaseURL = "http://localhost:8000"
	}

	userliToken := os.Getenv("USERLI_TOKEN")
	if userliToken == "" {
		log.Fatal("USERLI_TOKEN is required")
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
	}
}
