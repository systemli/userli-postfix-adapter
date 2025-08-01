package main

import (
	"context"
	"net"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	MaxConcurrentConnections = 500              // Limit concurrent connections
	ConnectionTimeout        = 30 * time.Second // Overall connection timeout
	ReadTimeout              = 10 * time.Second // Read operation timeout
	WriteTimeout             = 10 * time.Second // Write operation timeout
	KeepAliveTimeout         = 15 * time.Second // TCP Keep-Alive timeout
)

func StartTCPServer(ctx context.Context, wg *sync.WaitGroup, addr string, handler func(net.Conn)) {
	defer wg.Done()

	// Connection pool with semaphore pattern
	connSemaphore := make(chan struct{}, MaxConcurrentConnections)

	// Track active connections for graceful shutdown
	var activeConnections sync.WaitGroup

	lc := net.ListenConfig{
		KeepAlive: KeepAliveTimeout, // Enable TCP Keep-Alive for Postfix connections
	}

	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		log.WithError(err).WithField("addr", addr).Error("Failed to create listener")
		return
	}
	defer listener.Close()

	// Graceful shutdown handler
	go func() {
		<-ctx.Done()
		log.WithField("addr", addr).Info("Shutting down server...")
		listener.Close()

		// Wait for active connections to finish
		activeConnections.Wait()
		log.WithField("addr", addr).Info("All connections closed")
	}()

	log.WithField("addr", addr).Info("TCP server started")

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return // Context cancelled, exit gracefully
			}
			log.WithError(err).WithField("addr", addr).Error("Accept failed")
			continue
		}

		// Try to acquire connection slot (non-blocking)
		select {
		case connSemaphore <- struct{}{}:
			// Connection slot acquired
			activeConnections.Add(1)
			go handleConnection(conn, handler, connSemaphore, &activeConnections, addr)
		default:
			// Connection pool full, reject connection
			log.WithField("addr", addr).Warn("Connection pool full, rejecting connection")
			conn.Close()
		}
	}
}

func handleConnection(conn net.Conn, handler func(net.Conn), semaphore chan struct{}, wg *sync.WaitGroup, addr string) {
	defer func() {
		_ = conn.Close()
		<-semaphore // Release connection slot
		wg.Done()
	}()

	// Configure TCP Keep-Alive at connection level for better control
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(KeepAliveTimeout)
	}

	// Overall connection timeout for the entire session
	timer := time.AfterFunc(ConnectionTimeout, func() {
		log.WithField("addr", addr).Debug("Connection timeout, closing")
		_ = conn.Close()
	})
	defer timer.Stop()

	handler(conn)
}
