package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"math/big"
	"net"
	"testing"

	"github.com/stretchr/testify/suite"
)

type AliasTestSuite struct {
	suite.Suite
}

func (s *AliasTestSuite) TestAlias() {
	userli := new(MockUserliService)
	userli.On("GetAliases", "alias@example.com").Return([]string{"source1@example.com", "source2.example.com"}, nil)
	userli.On("GetAliases", "noalias@example.com").Return([]string{}, nil)
	userli.On("GetAliases", "error@example.com").Return([]string{}, errors.New("error"))

	portNumber, _ := rand.Int(rand.Reader, big.NewInt(65535-20000))
	listen := ":" + portNumber.String()

	alias := NewAlias(listen, userli)
	go alias.Listen()

	// wait until the server is ready
	for {
		conn, err := net.Dial("tcp", listen)
		if err == nil {
			conn.Close()
			break
		}
	}

	s.Run("success", func() {
		conn, err := net.Dial("tcp", listen)
		s.NoError(err)

		_, err = conn.Write([]byte("get alias@example.com"))
		s.NoError(err)

		response := make([]byte, 4096)
		_, err = conn.Read(response)
		s.NoError(err)

		s.Equal("200 source1@example.com,source2.example.com \n", string(bytes.Trim(response, "\x00")))

		conn.Close()
	})

	s.Run("empty result", func() {
		conn, err := net.Dial("tcp", listen)
		s.NoError(err)

		_, err = conn.Write([]byte("get noalias@example.com"))
		s.NoError(err)

		response := make([]byte, 4096)
		_, err = conn.Read(response)
		s.NoError(err)

		s.Equal("500 NO%20RESULT\n", string(bytes.Trim(response, "\x00")))

		conn.Close()
	})
}

func TestAlias(t *testing.T) {
	suite.Run(t, new(AliasTestSuite))
}