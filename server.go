package main

import (
	"context"
	"net"
	"sync"

	log "github.com/sirupsen/logrus"
)

func StartTCPServer(ctx context.Context, wg *sync.WaitGroup, addr string, handler func(net.Conn), maxWorkers int) {
	defer wg.Done()

	// Create a buffered channel to limit concurrent connections
	semaphore := make(chan struct{}, maxWorkers)

	// Create connection queue channel
	connQueue := make(chan net.Conn, maxWorkers)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.WithError(err).Error("Error creating listener")
		return
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	// Start worker pool
	for i := 0; i < maxWorkers; i++ {
		go worker(ctx, handler, connQueue, semaphore)
	}

	log.Info("Server started on ", addr, " with ", maxWorkers, " workers")

	// Accept connections
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

		// Try to acquire semaphore
		select {
		case semaphore <- struct{}{}:
			// Successfully acquired semaphore, queue the connection
			select {
			case connQueue <- conn:
			case <-ctx.Done():
				conn.Close()
				return
			}
		default:
			// If we can't acquire semaphore, we're at capacity
			log.Warn("Server at capacity, dropping connection")
			conn.Close()
		}
	}
}

func worker(ctx context.Context, handler func(net.Conn), connQueue chan net.Conn, semaphore chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case conn := <-connQueue:
			// Process the connection
			handler(conn)
			// Release the semaphore
			<-semaphore
		}
	}
}
