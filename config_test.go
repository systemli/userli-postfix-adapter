package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	log "github.com/sirupsen/logrus"
)

type ConfigTestSuite struct {
	suite.Suite
}

func (s *ConfigTestSuite) TestNewConfig() {
	s.Run("fail when userli token not set", func() {
		defer func() { log.StandardLogger().ExitFunc = nil }()
		var fatal bool
		log.StandardLogger().ExitFunc = func(int) { fatal = true }

		_ = NewConfig()

		s.True(fatal)
	})

	s.Run("default config", func() {
		os.Setenv("USERLI_TOKEN", "token")

		config := NewConfig()

		s.Equal("token", config.UserliToken)
		s.Equal("http://localhost:8000", config.UserliBaseURL)
		s.Equal(":10001", config.AliasListenAddr)
		s.Equal(":10002", config.DomainListenAddr)
		s.Equal(":10003", config.MailboxListenAddr)
		s.Equal(":10004", config.SendersListenAddr)
	})

	s.Run("custom config", func() {
		os.Setenv("USERLI_TOKEN", "token")
		os.Setenv("USERLI_BASE_URL", "http://example.com")
		os.Setenv("ALIAS_LISTEN_ADDR", ":20001")
		os.Setenv("DOMAIN_LISTEN_ADDR", ":20002")
		os.Setenv("MAILBOX_LISTEN_ADDR", ":20003")
		os.Setenv("SENDERS_LISTEN_ADDR", ":20004")

		config := NewConfig()

		s.Equal("token", config.UserliToken)
		s.Equal("http://example.com", config.UserliBaseURL)
		s.Equal(":20001", config.AliasListenAddr)
		s.Equal(":20002", config.DomainListenAddr)
		s.Equal(":20003", config.MailboxListenAddr)
		s.Equal(":20004", config.SendersListenAddr)
	})
}

func TestConfig(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}
