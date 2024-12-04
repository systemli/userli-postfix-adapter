package main

import (
	"context"
	"net"
	"sync"

	log "github.com/sirupsen/logrus"
)

func StartTCPServer(ctx context.Context, wg *sync.WaitGroup, addr string, handler func(net.Conn)) {
	defer wg.Done()

	lc := net.ListenConfig{
		KeepAlive: -1,
	}

	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		log.WithError(err).Error("Error creating listener")
		return
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	log.Info("Server started on ", addr)

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				log.Info("Server stopped on port ", addr)
				return
			}
			log.WithError(err).Error("Error accepting connection")
			continue
		}

		go func() {
			defer func() {
				log.Debug("Closing connection")
				if err := conn.Close(); err != nil {
					log.WithError(err).Error("Error closing connection")
				}
			}()

			handler(conn)
		}()
	}
}
