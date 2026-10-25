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
		s.Equal(2*time.Second, config.SASLFailureDelay)
		s.Equal(10, config.SASLMaxFailures)
		s.Equal(15*time.Minute, config.SASLFailureWindow)
	})

	s.Run("custom SASL throttle config", func() {
		s.T().Setenv("USERLI_TOKEN", "token")
		s.T().Setenv("REDIS_URL", "redis://localhost:6379/0")
		s.T().Setenv("SASL_FAILURE_DELAY", "1s")
		s.T().Setenv("SASL_MAX_FAILURES", "0")
		s.T().Setenv("SASL_FAILURE_WINDOW", "1h")

		config, err := NewConfig()

		s.NoError(err)
		s.Equal(time.Second, config.SASLFailureDelay)
		s.Equal(0, config.SASLMaxFailures)
		s.Equal(time.Hour, config.SASLFailureWindow)
	})

	s.Run("invalid SASL throttle config", func() {
		s.T().Setenv("USERLI_TOKEN", "token")
		s.T().Setenv("REDIS_URL", "redis://localhost:6379/0")

		for env, value := range map[string]string{
			"SASL_FAILURE_DELAY":  "10s",
			"SASL_MAX_FAILURES":   "-1",
			"SASL_FAILURE_WINDOW": "abc",
		} {
			s.T().Setenv(env, value)
			config, err := NewConfig()
			s.Nil(config, env)
			s.ErrorContains(err, env)
			os.Unsetenv(env)
		}
	})

	s.Run("custom config", func() {
		os.Setenv("USERLI_TOKEN", "token")
		os.Setenv("USERLI_BASE_URL", "http://example.com")
		os.Setenv("POSTFIX_RECIPIENT_DELIMITER", "+")
		os.Setenv("SOCKETMAP_LISTEN_ADDR", ":20001")
		os.Setenv("METRICS_LISTEN_ADDR", ":20002")
		os.Setenv("RATE_LIMIT_MESSAGE", "Too many emails")
		os.Setenv("REDIS_URL", "redis://redis:6379/1")

		config, err := NewConfig()

		s.NoError(err)
		s.Equal("token", config.UserliToken)
		s.Equal("http://example.com", config.UserliBaseURL)
		s.Equal("+", config.PostfixRecipientDelimiter)
		s.Equal(":20001", config.SocketmapListenAddr)
		s.Equal(":20002", config.MetricsListenAddr)
		s.Equal("Too many emails", config.RateLimitMessage)
		s.Equal("redis://redis:6379/1", config.RedisURL)
	})
}

func TestConfig(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}
