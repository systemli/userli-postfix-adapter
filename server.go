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
	ConnectionTimeout        = 60 * time.Second // Connection timeout
	KeepAliveTimeout         = 60 * time.Second // TCP Keep-Alive timeout
	ReadTimeout              = 10 * time.Second // Read operation timeout
	WriteTimeout             = 10 * time.Second // Write operation timeout
)

func StartSocketmapServer(ctx context.Context, wg *sync.WaitGroup, addr string, adapter *SocketmapAdapter) {
	defer wg.Done()

	// Connection pool with semaphore pattern
	connSemaphore := make(chan struct{}, MaxConcurrentConnections)

	// Track active connections for graceful shutdown
	var activeConnWg sync.WaitGroup

	lc := net.ListenConfig{
		KeepAlive: KeepAliveTimeout, // Enable TCP Keep-Alive for Postfix connections
	}

	listener, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		log.WithError(err).WithField("addr", addr).Error("Failed to create socketmap listener")
		return
	}
	defer listener.Close()

	// Graceful shutdown handler
	go func() {
		<-ctx.Done()
		log.WithField("addr", addr).Info("Shutting down socketmap server...")
		listener.Close()

		// Wait for active connections to finish
		activeConnWg.Wait()
		activeConnections.Set(0)
		connectionPoolUsage.Set(0)
		log.WithField("addr", addr).Info("All socketmap connections closed")
	}()

	log.WithField("addr", addr).Info("Socketmap server started")

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
			activeConnWg.Add(1)
			activeConnections.Inc()
			connectionPoolUsage.Set(float64(len(connSemaphore)))
			go handleSocketmapConnection(conn, adapter, connSemaphore, &activeConnWg)
		default:
			// Connection pool full, reject connection
			log.WithField("addr", addr).Warn("Connection pool full, rejecting socketmap connection")
			conn.Close()
		}
	}
}

func handleSocketmapConnection(conn net.Conn, adapter *SocketmapAdapter, semaphore chan struct{}, wg *sync.WaitGroup) {
	defer func() {
		_ = conn.Close()
		<-semaphore // Release connection slot
		activeConnections.Dec()
		connectionPoolUsage.Set(float64(len(semaphore)))
		wg.Done()
	}()

	// Configure TCP Keep-Alive at connection level for better control
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(KeepAliveTimeout)
	}

	// Set connection deadline - socketmap supports persistent connections
	deadline := time.Now().Add(ConnectionTimeout)
	_ = conn.SetDeadline(deadline)

	// Execute socketmap handler (handles multiple requests per connection)
	adapter.HandleConnection(conn)
}
