package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"
)

type AdapterTestSuite struct {
	suite.Suite

	ctx    context.Context
	wg     *sync.WaitGroup
	client UserliService
}

func (s *AdapterTestSuite) SetupTest() {
	s.wg = &sync.WaitGroup{}
	s.ctx = context.Background()
}

func (s *AdapterTestSuite) AfterTest(_, _ string) {
	s.ctx.Done()
}

func (s *AdapterTestSuite) TestAliasHandler() {
	userli := new(MockUserliService)
	userli.On("GetAliases", "alias@example.com").Return([]string{"source1@example.com", "source2.example.com"}, nil)
	userli.On("GetAliases", "noalias@example.com").Return([]string{}, nil)
	userli.On("GetAliases", "error@example.com").Return([]string{}, errors.New("error"))

	portNumber, _ := rand.Int(rand.Reader, big.NewInt(65535-20000))
	portNumber.Add(portNumber, big.NewInt(20000))
	listen := ":" + portNumber.String()

	adapter := NewPostfixAdapter(userli)

	go StartTCPServer(s.ctx, s.wg, listen, adapter.AliasHandler)

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

	s.Run("error", func() {
		conn, err := net.Dial("tcp", listen)
		s.NoError(err)

		_, err = conn.Write([]byte("get error@example.com"))
		s.NoError(err)

		response := make([]byte, 4096)
		_, err = conn.Read(response)
		s.NoError(err)
		s.Equal("400 Error fetching aliases\n", string(bytes.Trim(response, "\x00")))
	})
}

func (s *AdapterTestSuite) TestDomainHandler() {
	userli := new(MockUserliService)
	userli.On("GetDomain", "example.com").Return(true, nil)
	userli.On("GetDomain", "notfound.com").Return(false, nil)
	userli.On("GetDomain", "error.com").Return(false, errors.New("error"))

	portNumber, _ := rand.Int(rand.Reader, big.NewInt(65535-20000))
	portNumber.Add(portNumber, big.NewInt(20000))
	listen := ":" + portNumber.String()

	adapter := NewPostfixAdapter(userli)

	go StartTCPServer(s.ctx, s.wg, listen, adapter.DomainHandler)

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

		_, err = conn.Write([]byte("get example.com"))
		s.NoError(err)

		response := make([]byte, 4096)
		_, err = conn.Read(response)
		s.NoError(err)

		s.Equal("200 1\n", string(bytes.Trim(response, "\x00")))

		conn.Close()
	})

	s.Run("not found", func() {
		conn, err := net.Dial("tcp", listen)
		s.NoError(err)

		_, err = conn.Write([]byte("get notfound.com"))
		s.NoError(err)

		response := make([]byte, 4096)
		_, err = conn.Read(response)
		s.NoError(err)

		s.Equal("500 NO%20RESULT\n", string(bytes.Trim(response, "\x00")))

		conn.Close()
	})

	s.Run("error", func() {
		conn, err := net.Dial("tcp", listen)
		s.NoError(err)

		_, err = conn.Write([]byte("get error.com"))
		s.NoError(err)

		response := make([]byte, 4096)
		_, err = conn.Read(response)
		s.NoError(err)

		s.Equal("400 Error fetching domain\n", string(bytes.Trim(response, "\x00")))

		conn.Close()
	})
}

func TestAdapterTestSuite(t *testing.T) {
	suite.Run(t, new(AdapterTestSuite))
}