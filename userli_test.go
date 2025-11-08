package main

import (
	"context"
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

		aliases, err := s.userli.GetAliases(context.Background(), "alias@example.com")
		s.NoError(err)
		s.True(gock.IsDone())
		s.Equal([]string{"source1@example.com", "source2@example.com"}, aliases)
	})

	s.Run("no email", func() {
		aliases, err := s.userli.GetAliases(context.Background(), "alias")
		s.Error(err)
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

		aliases, err := s.userli.GetAliases(context.Background(), "alias@example.com")
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

		active, err := s.userli.GetDomain(context.Background(), "example.com")
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

		active, err := s.userli.GetDomain(context.Background(), "example.com")
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

		active, err := s.userli.GetDomain(context.Background(), "example.com")
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

		active, err := s.userli.GetMailbox(context.Background(), "user@example.org")
		s.NoError(err)
		s.True(active)
		s.True(gock.IsDone())
	})

	s.Run("no email", func() {
		active, err := s.userli.GetMailbox(context.Background(), "user")
		s.Error(err)
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

		active, err := s.userli.GetMailbox(context.Background(), "user@example.org")
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

		active, err := s.userli.GetMailbox(context.Background(), "user@example.org")
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

		senders, err := s.userli.GetSenders(context.Background(), "user@example.com")
		s.NoError(err)
		s.Equal([]string{"user@example.com"}, senders)
		s.True(gock.IsDone())
	})

	s.Run("no email", func() {
		senders, err := s.userli.GetSenders(context.Background(), "user")
		s.Error(err)
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

		senders, err := s.userli.GetSenders(context.Background(), "alias@example.com")
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

		senders, err := s.userli.GetSenders(context.Background(), "user@example.com")
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

func (s *UserliTestSuite) TestWithDelimiter() {
	s.Run("sets delimiter", func() {
		delimiter := "+"
		userli := NewUserli("token", "http://localhost", WithDelimiter(delimiter))

		userli.mu.RLock()
		configuredDelimiter := userli.delimiter
		userli.mu.RUnlock()

		s.Equal(delimiter, configuredDelimiter)
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

func (s *UserliTestSuite) TestSanitizeEmail() {
	s.Run("valid email without delimiter", func() {
		result, err := s.userli.sanitizeEmail("user@example.com")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("SRS0 address is rejected due to invalid characters", func() {
		result, err := s.userli.sanitizeEmail("SRS0=hash=domain.com=user@forwarder.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid local part")
	})

	s.Run("SRS1 address is rejected due to invalid characters", func() {
		result, err := s.userli.sanitizeEmail("SRS1=hash=domain.com=user@forwarder.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid local part")
	})

	s.Run("lowercase srs0 address is rejected due to invalid characters", func() {
		result, err := s.userli.sanitizeEmail("srs0=hash=domain.com=user@forwarder.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid local part")
	})

	s.Run("lowercase srs1 address is rejected due to invalid characters", func() {
		result, err := s.userli.sanitizeEmail("srs1=hash=domain.com=user@forwarder.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid local part")
	})

	s.Run("mixed case SrS0 address is rejected due to invalid characters", func() {
		result, err := s.userli.sanitizeEmail("SrS0=hash=domain.com=user@forwarder.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid local part")
	})

	s.Run("constant contact address is rejected due to invalid characters", func() {
		result, err := s.userli.sanitizeEmail("a1vdek4eqtvi/ni3ked62sg==_1103304917473_t+iblfa0ee+fkpowpmt8pw==@in.constantcontact.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid local part")
	})

	s.Run("normalizes to lowercase", func() {
		result, err := s.userli.sanitizeEmail("User@Example.COM")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("removes leading and trailing whitespace", func() {
		result, err := s.userli.sanitizeEmail("  user@example.com  ")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("removes zero-width space", func() {
		// Zero-width space (U+200B) at beginning and end
		result, err := s.userli.sanitizeEmail("\u200Buser@example.com\u200B")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("removes zero-width non-joiner", func() {
		// Zero-width non-joiner (U+200C)
		result, err := s.userli.sanitizeEmail("\u200Cuser@example.com\u200C")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("removes zero-width joiner", func() {
		// Zero-width joiner (U+200D)
		result, err := s.userli.sanitizeEmail("\u200Duser@example.com\u200D")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("removes BOM (zero-width no-break space)", func() {
		// BOM / zero-width no-break space (U+FEFF)
		result, err := s.userli.sanitizeEmail("\uFEFFuser@example.com\uFEFF")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("removes control characters", func() {
		// ASCII control characters (tab, newline, carriage return)
		result, err := s.userli.sanitizeEmail("\tuser@example.com\n")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("removes DEL character", func() {
		// DEL character (127)
		result, err := s.userli.sanitizeEmail("\x7Fuser@example.com\x7F")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("combined normalization: uppercase, whitespace, and control chars", func() {
		result, err := s.userli.sanitizeEmail("  \tUser@Example.COM\n\u200B  ")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("invalid email missing @", func() {
		result, err := s.userli.sanitizeEmail("userexample.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid email format")
	})

	s.Run("invalid email multiple @", func() {
		result, err := s.userli.sanitizeEmail("user@domain@example.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid email format")
	})

	s.Run("empty email", func() {
		result, err := s.userli.sanitizeEmail("")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid email format")
	})

	s.Run("removes delimiter when configured", func() {
		userli := NewUserli("insecure", "http://localhost:8000", WithDelimiter("+"))
		result, err := userli.sanitizeEmail("user+tag@example.com")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("no delimiter configured rejects plus sign", func() {
		result, err := s.userli.sanitizeEmail("user+tag@example.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid local part")
	})

	s.Run("removes delimiter with multiple occurrences", func() {
		userli := NewUserli("insecure", "http://localhost:8000", WithDelimiter("+"))
		result, err := userli.sanitizeEmail("user+tag+extra@example.com")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("delimiter at end of local part", func() {
		userli := NewUserli("insecure", "http://localhost:8000", WithDelimiter("+"))
		result, err := userli.sanitizeEmail("user+@example.com")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("delimiter at start of local part results in error", func() {
		userli := NewUserli("insecure", "http://localhost:8000", WithDelimiter("+"))
		result, err := userli.sanitizeEmail("+tag@example.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "empty local part after sanitization")
	})

	s.Run("different delimiter character", func() {
		userli := NewUserli("insecure", "http://localhost:8000", WithDelimiter("-"))
		result, err := userli.sanitizeEmail("user-tag@example.com")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("delimiter not present in email", func() {
		userli := NewUserli("insecure", "http://localhost:8000", WithDelimiter("+"))
		result, err := userli.sanitizeEmail("user@example.com")
		s.NoError(err)
		s.Equal("user@example.com", result)
	})

	s.Run("preserves domain with dots", func() {
		result, err := s.userli.sanitizeEmail("user@mail.example.com")
		s.NoError(err)
		s.Equal("user@mail.example.com", result)
	})

	s.Run("preserves local part with dots", func() {
		result, err := s.userli.sanitizeEmail("user.name@example.com")
		s.NoError(err)
		s.Equal("user.name@example.com", result)
	})

	s.Run("allows hyphens in local part", func() {
		result, err := s.userli.sanitizeEmail("user-name@example.com")
		s.NoError(err)
		s.Equal("user-name@example.com", result)
	})

	s.Run("allows underscores in local part", func() {
		result, err := s.userli.sanitizeEmail("user_name@example.com")
		s.NoError(err)
		s.Equal("user_name@example.com", result)
	})

	s.Run("allows numbers in local part", func() {
		result, err := s.userli.sanitizeEmail("user123@example.com")
		s.NoError(err)
		s.Equal("user123@example.com", result)
	})

	s.Run("allows alphanumeric with dots, hyphens, underscores", func() {
		result, err := s.userli.sanitizeEmail("user.name-123_test@example.com")
		s.NoError(err)
		s.Equal("user.name-123_test@example.com", result)
	})

	s.Run("rejects local part with slash", func() {
		result, err := s.userli.sanitizeEmail("user/name@example.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid local part")
	})

	s.Run("rejects local part with equals sign", func() {
		result, err := s.userli.sanitizeEmail("user=name@example.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid local part")
	})

	s.Run("rejects local part with special characters", func() {
		result, err := s.userli.sanitizeEmail("user!name@example.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid local part")
	})

	s.Run("rejects local part with hash symbol", func() {
		result, err := s.userli.sanitizeEmail("user#name@example.com")
		s.Error(err)
		s.Equal("", result)
		s.Contains(err.Error(), "invalid local part")
	})
}

func TestUserl(t *testing.T) {
	suite.Run(t, new(UserliTestSuite))
}
