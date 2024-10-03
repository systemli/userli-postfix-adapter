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

	// AliasListenAddr is the address to listen for alias requests.
	AliasListenAddr string

	// DomainListenAddr is the address to listen for domain requests.
	DomainListenAddr string

	// MailboxListenAddr is the address to listen for mailbox requests.
	MailboxListenAddr string

	// SendersListenAddr is the address to listen for senders requests.
	SendersListenAddr string
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

	aliasListenAddr := os.Getenv("ALIAS_LISTEN_ADDR")
	if aliasListenAddr == "" {
		aliasListenAddr = ":10001"
	}

	domainListenAddr := os.Getenv("DOMAIN_LISTEN_ADDR")
	if domainListenAddr == "" {
		domainListenAddr = ":10002"
	}

	mailboxListenAddr := os.Getenv("MAILBOX_LISTEN_ADDR")
	if mailboxListenAddr == "" {
		mailboxListenAddr = ":10003"
	}

	sendersListenAddr := os.Getenv("SENDERS_LISTEN_ADDR")
	if sendersListenAddr == "" {
		sendersListenAddr = ":10004"
	}

	return &Config{
		UserliBaseURL:     userliBaseURL,
		UserliToken:       userliToken,
		AliasListenAddr:   aliasListenAddr,
		DomainListenAddr:  domainListenAddr,
		MailboxListenAddr: mailboxListenAddr,
		SendersListenAddr: sendersListenAddr,
	}
}
