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

type DomainTestSuite struct {
	suite.Suite

	wg  *sync.WaitGroup
	ctx context.Context
}

func (s *DomainTestSuite) SetupTest() {
	s.wg = &sync.WaitGroup{}
	s.ctx = context.Background()
}

func (s *DomainTestSuite) AfterTest(_, _ string) {
	s.ctx.Done()
}

func (s *DomainTestSuite) TestDomain() {
	userli := new(MockUserliService)
	userli.On("GetDomain", "example.com").Return(true, nil)
	userli.On("GetDomain", "notfound.com").Return(false, nil)
	userli.On("GetDomain", "error.com").Return(false, errors.New("error"))

	portNumber, _ := rand.Int(rand.Reader, big.NewInt(65535-20000))
	listen := ":" + portNumber.String()

	domain := NewDomain(userli)

	go StartTCPServer(s.ctx, s.wg, listen, domain.Handle)

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

func TestDomain(t *testing.T) {
	suite.Run(t, new(DomainTestSuite))
}
