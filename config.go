package main

import (
	"os"
	"strconv"

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

	// AliasMaxWorkers is the maximum number of workers for the alias server.
	AliasMaxWorkers int

	// DomainListenAddr is the address to listen for domain requests.
	DomainListenAddr string

	// DomainMaxWorkers is the maximum number of workers for the domain server.
	DomainMaxWorkers int

	// MailboxListenAddr is the address to listen for mailbox requests.
	MailboxListenAddr string

	// MailboxMaxWorkers is the maximum number of workers for the mailbox server.
	MailboxMaxWorkers int

	// SendersListenAddr is the address to listen for senders requests.
	SendersListenAddr string

	// SendersMaxWorkers is the maximum number of workers for the senders server.
	SendersMaxWorkers int

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

	aliasListenAddr := os.Getenv("ALIAS_LISTEN_ADDR")
	if aliasListenAddr == "" {
		aliasListenAddr = ":10001"
	}

	aliasMaxWorkers, err := strconv.Atoi(os.Getenv("ALIAS_MAX_WORKERS"))
	if err != nil || aliasMaxWorkers <= 0 {
		aliasMaxWorkers = 10
	}

	domainListenAddr := os.Getenv("DOMAIN_LISTEN_ADDR")
	if domainListenAddr == "" {
		domainListenAddr = ":10002"
	}

	domainMaxWorkers, err := strconv.Atoi(os.Getenv("DOMAIN_MAX_WORKERS"))
	if err != nil || domainMaxWorkers <= 0 {
		domainMaxWorkers = 10
	}

	mailboxListenAddr := os.Getenv("MAILBOX_LISTEN_ADDR")
	if mailboxListenAddr == "" {
		mailboxListenAddr = ":10003"
	}

	mailboxMaxWorkers, err := strconv.Atoi(os.Getenv("MAILBOX_MAX_WORKERS"))
	if err != nil || mailboxMaxWorkers <= 0 {
		mailboxMaxWorkers = 10
	}

	sendersListenAddr := os.Getenv("SENDERS_LISTEN_ADDR")
	if sendersListenAddr == "" {
		sendersListenAddr = ":10004"
	}

	sendersMaxWorkers, err := strconv.Atoi(os.Getenv("SENDERS_MAX_WORKERS"))
	if err != nil || sendersMaxWorkers <= 0 {
		sendersMaxWorkers = 10
	}

	metricsListenAddr := os.Getenv("METRICS_LISTEN_ADDR")
	if metricsListenAddr == "" {
		metricsListenAddr = ":10005"
	}

	return &Config{
		UserliBaseURL:     userliBaseURL,
		UserliToken:       userliToken,
		AliasListenAddr:   aliasListenAddr,
		AliasMaxWorkers:   aliasMaxWorkers,
		DomainListenAddr:  domainListenAddr,
		DomainMaxWorkers:  domainMaxWorkers,
		MailboxListenAddr: mailboxListenAddr,
		MailboxMaxWorkers: mailboxMaxWorkers,
		SendersListenAddr: sendersListenAddr,
		SendersMaxWorkers: sendersMaxWorkers,
		MetricsListenAddr: metricsListenAddr,
	}
}
