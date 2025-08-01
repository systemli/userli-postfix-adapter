package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/h2non/gock"
	"github.com/stretchr/testify/suite"
)

type UserliTestSuite struct {
	suite.Suite

	userli *Userli
}

func (s *UserliTestSuite) SetupTest() {
	s.userli = NewUserli("insecure", "http://localhost:8000", WithClient(http.DefaultClient))

	gock.DisableNetworking()
}

func (s *UserliTestSuite) TearDownTest() {
	gock.Off()
}

func (s *UserliTestSuite) TestGetAliases() {
	s.Run("success", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/alias/alias@example.com").
			MatchHeader("Authorization", "Bearer insecure").
			MatchHeader("Accept", "application/json").
			MatchHeader("Content-Type", "application/json").
			MatchHeader("User-Agent", "userli-postfix-adapter").
			Reply(200).
			JSON([]string{"source1@example.com", "source2@example.com"})

		aliases, err := s.userli.GetAliases("alias@example.com")
		s.NoError(err)
		s.True(gock.IsDone())
		s.Equal([]string{"source1@example.com", "source2@example.com"}, aliases)
	})

	s.Run("no email", func() {
		aliases, err := s.userli.GetAliases("alias")
		s.NoError(err)
		s.Empty(aliases)
	})

	s.Run("error", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/alias/alias@example.com").
			MatchHeader("Authorization", "Bearer insecure").
			MatchHeader("Accept", "application/json").
			MatchHeader("Content-Type", "application/json").
			MatchHeader("User-Agent", "userli-postfix-adapter").
			Reply(500).
			JSON(map[string]string{"error": "internal server error"})

		aliases, err := s.userli.GetAliases("alias@example.com")
		s.Error(err)
		s.True(gock.IsDone())
		s.Empty(aliases)
	})
}

func (s *UserliTestSuite) TestGetDomain() {
	s.Run("success", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/domain/example.com").
			MatchHeader("Authorization", "Bearer insecure").
			MatchHeader("Accept", "application/json").
			MatchHeader("Content-Type", "application/json").
			MatchHeader("User-Agent", "userli-postfix-adapter").
			Reply(200).
			JSON("true")

		active, err := s.userli.GetDomain("example.com")
		s.NoError(err)
		s.True(active)
	})

	s.Run("not found", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/domain/example.com").
			MatchHeader("Authorization", "Bearer insecure").
			MatchHeader("Accept", "application/json").
			MatchHeader("Content-Type", "application/json").
			MatchHeader("User-Agent", "userli-postfix-adapter").
			Reply(200).
			JSON("false")

		active, err := s.userli.GetDomain("example.com")
		s.NoError(err)
		s.True(gock.IsDone())
		s.False(active)
	})

	s.Run("error", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/domain/example.com").
			MatchHeader("Authorization", "Bearer insecure").
			MatchHeader("Accept", "application/json").
			MatchHeader("Content-Type", "application/json").
			MatchHeader("User-Agent", "userli-postfix-adapter").
			Reply(500).
			JSON(map[string]string{"error": "internal server error"})

		active, err := s.userli.GetDomain("example.com")
		s.Error(err)
		s.True(gock.IsDone())
		s.False(active)
	})
}

func (s *UserliTestSuite) TestGetMailbox() {
	s.Run("success", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/mailbox/user@example.org").
			MatchHeader("Authorization", "Bearer insecure").
			MatchHeader("Accept", "application/json").
			MatchHeader("Content-Type", "application/json").
			MatchHeader("User-Agent", "userli-postfix-adapter").
			Reply(200).
			JSON("true")

		active, err := s.userli.GetMailbox("user@example.org")
		s.NoError(err)
		s.True(active)
		s.True(gock.IsDone())
	})

	s.Run("no email", func() {
		active, err := s.userli.GetMailbox("user")
		s.NoError(err)
		s.False(active)
	})

	s.Run("not found", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/mailbox/user@example.org").
			MatchHeader("Authorization", "Bearer insecure").
			MatchHeader("Accept", "application/json").
			MatchHeader("Content-Type", "application/json").
			MatchHeader("User-Agent", "userli-postfix-adapter").
			Reply(200).
			JSON("false")

		active, err := s.userli.GetMailbox("user@example.org")
		s.NoError(err)
		s.False(active)
		s.True(gock.IsDone())
	})

	s.Run("error", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/mailbox/user@example.org").
			MatchHeader("Authorization", "Bearer insecure").
			MatchHeader("Accept", "application/json").
			MatchHeader("Content-Type", "application/json").
			MatchHeader("User-Agent", "userli-postfix-adapter").
			Reply(500).
			JSON(map[string]string{"error": "internal server error"})

		active, err := s.userli.GetMailbox("user@example.org")
		s.Error(err)
		s.False(active)
		s.True(gock.IsDone())
	})
}

func (s *UserliTestSuite) TestGetSenders() {
	s.Run("success", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/senders/user@example.com").
			MatchHeader("Authorization", "Bearer insecure").
			MatchHeader("Accept", "application/json").
			MatchHeader("Content-Type", "application/json").
			MatchHeader("User-Agent", "userli-postfix-adapter").
			Reply(200).
			JSON([]string{"user@example.com"})

		senders, err := s.userli.GetSenders("user@example.com")
		s.NoError(err)
		s.Equal([]string{"user@example.com"}, senders)
		s.True(gock.IsDone())
	})

	s.Run("no email", func() {
		senders, err := s.userli.GetSenders("user")
		s.NoError(err)
		s.Empty(senders)
	})

	s.Run("alias success", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/senders/alias@example.com").
			MatchHeader("Authorization", "Bearer insecure").
			MatchHeader("Accept", "application/json").
			MatchHeader("Content-Type", "application/json").
			MatchHeader("User-Agent", "userli-postfix-adapter").
			Reply(200).
			JSON([]string{"user1@example.com", "user2@example.com"})

		senders, err := s.userli.GetSenders("alias@example.com")
		s.NoError(err)
		s.Equal([]string{"user1@example.com", "user2@example.com"}, senders)
		s.True(gock.IsDone())
	})

	s.Run("error", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/senders/user@example.com").
			MatchHeader("Authorization", "Bearer insecure").
			MatchHeader("Accept", "application/json").
			MatchHeader("Content-Type", "application/json").
			MatchHeader("User-Agent", "userli-postfix-adapter").
			Reply(500).
			JSON(map[string]string{"error": "internal server error"})

		senders, err := s.userli.GetSenders("user@example.com")
		s.Error(err)
		s.Empty(senders)
		s.True(gock.IsDone())
	})
}

func (s *UserliTestSuite) TestWithClient() {
	s.Run("sets custom client", func() {
		customClient := &http.Client{}
		userli := NewUserli("token", "http://localhost", WithClient(customClient))

		userli.mu.RLock()
		client := userli.Client
		userli.mu.RUnlock()

		s.Equal(customClient, client)
	})
}

func (s *UserliTestSuite) TestWithTransport() {
	s.Run("sets custom transport", func() {
		customTransport := &http.Transport{}
		userli := NewUserli("token", "http://localhost", WithTransport(customTransport))

		userli.mu.RLock()
		transport := userli.Client.Transport
		userli.mu.RUnlock()

		s.Equal(customTransport, transport)
		s.Equal(10*time.Second, userli.Client.Timeout) // 10 seconds in nanoseconds
	})
}

func (s *UserliTestSuite) TestWithTimeout() {
	s.Run("sets custom timeout with new client", func() {
		timeout := 30 * 1000000000 // 30 seconds in nanoseconds
		userli := NewUserli("token", "http://localhost", WithTimeout(30*1000000000))

		userli.mu.RLock()
		clientTimeout := userli.Client.Timeout
		userli.mu.RUnlock()

		s.Equal(timeout, int(clientTimeout))
		s.NotNil(userli.Client.Transport)
	})

	s.Run("preserves existing transport", func() {
		customTransport := &http.Transport{}
		userli := NewUserli("token", "http://localhost",
			WithTransport(customTransport),
			WithTimeout(20*time.Second)) // 20 seconds

		userli.mu.RLock()
		transport := userli.Client.Transport
		timeout := userli.Client.Timeout
		userli.mu.RUnlock()

		s.Equal(customTransport, transport)
		s.Equal(20*time.Second, timeout)
	})
}

func (s *UserliTestSuite) TestConcurrentOptions() {
	s.Run("thread safety", func() {
		userli := NewUserli("token", "http://localhost")

		// Test concurrent option applications
		done := make(chan bool, 3)

		go func() {
			WithTimeout(15 * time.Second)(userli) // 15 seconds
			done <- true
		}()

		go func() {
			WithClient(&http.Client{})(userli)
			done <- true
		}()

		go func() {
			WithTransport(&http.Transport{})(userli)
			done <- true
		}()

		// Wait for all goroutines to complete
		<-done
		<-done
		<-done

		// Verify client is set and accessible
		userli.mu.RLock()
		client := userli.Client
		userli.mu.RUnlock()

		s.NotNil(client)
	})
}

func TestUserl(t *testing.T) {
	suite.Run(t, new(UserliTestSuite))
}
