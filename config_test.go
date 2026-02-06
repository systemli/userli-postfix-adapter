package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	"go.uber.org/zap"
)

type ConfigTestSuite struct {
	suite.Suite
}

func (s *ConfigTestSuite) SetupTest() {
	logger = zap.NewNop()
}

func (s *ConfigTestSuite) TestNewConfig() {
	s.Run("fail when userli token not set", func() {
		os.Unsetenv("USERLI_TOKEN")

		config, err := NewConfig()

		s.Nil(config)
		s.Error(err)
		s.Contains(err.Error(), "USERLI_TOKEN is required")
	})

	s.Run("default config", func() {
		os.Setenv("USERLI_TOKEN", "token")

		config, err := NewConfig()

		s.NoError(err)
		s.Equal("token", config.UserliToken)
		s.Equal("http://localhost:8000", config.UserliBaseURL)
		s.Equal("", config.PostfixRecipientDelimiter)
		s.Equal(":10001", config.SocketmapListenAddr)
		s.Equal(":10002", config.MetricsListenAddr)
	})

	s.Run("custom config", func() {
		os.Setenv("USERLI_TOKEN", "token")
		os.Setenv("USERLI_BASE_URL", "http://example.com")
		os.Setenv("POSTFIX_RECIPIENT_DELIMITER", "+")
		os.Setenv("SOCKETMAP_LISTEN_ADDR", ":20001")
		os.Setenv("METRICS_LISTEN_ADDR", ":20002")

		config, err := NewConfig()

		s.NoError(err)
		s.Equal("token", config.UserliToken)
		s.Equal("http://example.com", config.UserliBaseURL)
		s.Equal("+", config.PostfixRecipientDelimiter)
		s.Equal(":20001", config.SocketmapListenAddr)
		s.Equal(":20002", config.MetricsListenAddr)
	})
}

func TestConfig(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}
