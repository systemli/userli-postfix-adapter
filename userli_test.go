package main

import (
	"testing"

	"github.com/h2non/gock"
	"github.com/stretchr/testify/suite"
)

type UserliTestSuite struct {
	suite.Suite

	userli *Userli
}

func (s *UserliTestSuite) SetupTest() {
	s.userli = NewUserli("insecure", "http://localhost:8000")

	gock.DisableNetworking()
	defer gock.Off()
}

func (s *UserliTestSuite) TestGetAliases() {
	s.Run("success", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/alias/alias@example.com").
			MatchHeader("Authentication", "Bearer insecure").
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

	s.Run("error", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/alias/alias@example.com").
			MatchHeader("Authentication", "Bearer insecure").
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
			MatchHeader("Authentication", "Bearer insecure").
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
			MatchHeader("Authentication", "Bearer insecure").
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
			MatchHeader("Authentication", "Bearer insecure").
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

func TestUserl(t *testing.T) {
	suite.Run(t, new(UserliTestSuite))
}
