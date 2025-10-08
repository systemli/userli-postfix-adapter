package main

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	log "github.com/sirupsen/logrus"
)

type ConfigTestSuite struct {
	suite.Suite
}

func (s *ConfigTestSuite) SetupTest() {
	log.SetOutput(io.Discard)
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
		s.Equal(":10001", config.SocketmapListenAddr)
		s.Equal(":10002", config.MetricsListenAddr)
	})

	s.Run("custom config", func() {
		os.Setenv("USERLI_TOKEN", "token")
		os.Setenv("USERLI_BASE_URL", "http://example.com")
		os.Setenv("SOCKETMAP_LISTEN_ADDR", ":20001")
		os.Setenv("METRICS_LISTEN_ADDR", ":20002")

		config := NewConfig()

		s.Equal("token", config.UserliToken)
		s.Equal("http://example.com", config.UserliBaseURL)
		s.Equal(":20001", config.SocketmapListenAddr)
		s.Equal(":20002", config.MetricsListenAddr)
	})
}

func TestConfig(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}
