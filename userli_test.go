package main

import (
	"testing"

	"github.com/h2non/gock"
	"github.com/stretchr/testify/suite"
)

type UserliTestSuite struct {
	suite.Suite
}

func (s *UserliTestSuite) SetupTest() {
	gock.DisableNetworking()
	defer gock.Off()
}

func (s *UserliTestSuite) TestGetAliases() {
	userli := NewUserli("insecure", "http://localhost:8000")

	s.Run("success", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/alias/alias@example.com").
			Reply(200).
			JSON([]string{"source1@example.com", "source2@example.com"})

		aliases, err := userli.GetAliases("alias@example.com")
		s.NoError(err)
		s.Equal([]string{"source1@example.com", "source2@example.com"}, aliases)
	})

	s.Run("error", func() {
		gock.New("http://localhost:8000").
			Get("/api/postfix/alias/alias@example.com").
			Reply(500).
			JSON(map[string]string{"error": "internal server error"})

		aliases, err := userli.GetAliases("alias@example.com")
		s.Error(err)
		s.Empty(aliases)
	})
}

func TestUserl(t *testing.T) {
	suite.Run(t, new(UserliTestSuite))
}
