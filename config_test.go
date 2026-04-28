package main

import (
	"os"
	"testing"
	"time"

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
		os.Unsetenv("REDIS_URL")

		config, err := NewConfig()

		s.Nil(config)
		s.Error(err)
		s.Contains(err.Error(), "USERLI_TOKEN is required")
	})

	s.Run("fail when redis url not set", func() {
		os.Setenv("USERLI_TOKEN", "token")
		os.Unsetenv("REDIS_URL")

		config, err := NewConfig()

		s.Nil(config)
		s.Error(err)
		s.Contains(err.Error(), "REDIS_URL is required")
	})

	s.Run("default config", func() {
		os.Setenv("USERLI_TOKEN", "token")
		os.Setenv("REDIS_URL", "redis://localhost:6379/0")

		config, err := NewConfig()

		s.NoError(err)
		s.Equal("token", config.UserliToken)
		s.Equal("http://localhost:8000", config.UserliBaseURL)
		s.Equal("", config.PostfixRecipientDelimiter)
		s.Equal(":10001", config.SocketmapListenAddr)
		s.Equal(":10002", config.MetricsListenAddr)
		s.Equal("Rate limit exceeded, please try again later", config.RateLimitMessage)
		s.Equal("redis://localhost:6379/0", config.RedisURL)
		s.Equal("localhost", config.TLSPolicyEhloHostname)
		s.Equal(168*time.Hour, config.TLSPolicyCacheTTLTLS)
		s.Equal(24*time.Hour, config.TLSPolicyCacheTTLNoTLS)
	})

	s.Run("custom config", func() {
		os.Setenv("USERLI_TOKEN", "token")
		os.Setenv("USERLI_BASE_URL", "http://example.com")
		os.Setenv("POSTFIX_RECIPIENT_DELIMITER", "+")
		os.Setenv("SOCKETMAP_LISTEN_ADDR", ":20001")
		os.Setenv("METRICS_LISTEN_ADDR", ":20002")
		os.Setenv("RATE_LIMIT_MESSAGE", "Too many emails")
		os.Setenv("REDIS_URL", "redis://redis:6379/1")
		os.Setenv("TLS_POLICY_EHLO_HOSTNAME", "mail.example.org")
		os.Setenv("TLS_POLICY_CACHE_TTL_TLS", "48h")
		os.Setenv("TLS_POLICY_CACHE_TTL_NOTLS", "12h")
		defer func() {
			os.Unsetenv("TLS_POLICY_EHLO_HOSTNAME")
			os.Unsetenv("TLS_POLICY_CACHE_TTL_TLS")
			os.Unsetenv("TLS_POLICY_CACHE_TTL_NOTLS")
		}()

		config, err := NewConfig()

		s.NoError(err)
		s.Equal("token", config.UserliToken)
		s.Equal("http://example.com", config.UserliBaseURL)
		s.Equal("+", config.PostfixRecipientDelimiter)
		s.Equal(":20001", config.SocketmapListenAddr)
		s.Equal(":20002", config.MetricsListenAddr)
		s.Equal("Too many emails", config.RateLimitMessage)
		s.Equal("redis://redis:6379/1", config.RedisURL)
		s.Equal("mail.example.org", config.TLSPolicyEhloHostname)
		s.Equal(48*time.Hour, config.TLSPolicyCacheTTLTLS)
		s.Equal(12*time.Hour, config.TLSPolicyCacheTTLNoTLS)
	})
}

func TestConfig(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}
