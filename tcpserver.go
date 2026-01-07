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

// TCPServerConfig holds configuration for a TCP server
type TCPServerConfig struct {
	Name                 string
	Addr                 string
	OnConnectionAcquired func()
	OnConnectionReleased func()
	OnConnectionPoolFull func()
}

// ConnectionHandler is the interface for handling TCP connections
type ConnectionHandler interface {
	HandleConnection(conn net.Conn)
}

// StartTCPServer starts a TCP server with connection pooling and graceful shutdown
func StartTCPServer(ctx context.Context, wg *sync.WaitGroup, config TCPServerConfig, handler ConnectionHandler) {
	defer wg.Done()

	connSemaphore := make(chan struct{}, MaxConcurrentConnections)
	var activeConnWg sync.WaitGroup

	lc := net.ListenConfig{
		KeepAlive: KeepAliveTimeout,
	}

	listener, err := lc.Listen(ctx, "tcp", config.Addr)
	if err != nil {
		log.WithError(err).WithField("addr", config.Addr).Errorf("Failed to create %s listener", config.Name)
		return
	}
	defer listener.Close()

	// Graceful shutdown handler
	go func() {
		<-ctx.Done()
		log.WithField("addr", config.Addr).Infof("Shutting down %s server...", config.Name)
		listener.Close()
		activeConnWg.Wait()
		log.WithField("addr", config.Addr).Infof("All %s connections closed", config.Name)
	}()

	log.WithField("addr", config.Addr).Infof("%s server started", config.Name)

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.WithError(err).WithField("addr", config.Addr).Error("Accept failed")
			continue
		}

		select {
		case connSemaphore <- struct{}{}:
			activeConnWg.Add(1)
			if config.OnConnectionAcquired != nil {
				config.OnConnectionAcquired()
			}
			go handleTCPConnection(conn, handler, connSemaphore, &activeConnWg, config.OnConnectionReleased)
		default:
			log.WithField("addr", config.Addr).Warnf("Connection pool full, rejecting %s connection", config.Name)
			if config.OnConnectionPoolFull != nil {
				config.OnConnectionPoolFull()
			}
			conn.Close()
		}
	}
}

// handleTCPConnection manages a single TCP connection with proper cleanup
func handleTCPConnection(conn net.Conn, handler ConnectionHandler, semaphore chan struct{}, wg *sync.WaitGroup, onReleased func()) {
	defer func() {
		conn.Close()
		<-semaphore
		if onReleased != nil {
			onReleased()
		}
		wg.Done()
	}()

	// Configure TCP Keep-Alive at connection level
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(KeepAliveTimeout)
	}

	// Set connection deadline
	_ = conn.SetDeadline(time.Now().Add(ConnectionTimeout))

	handler.HandleConnection(conn)
}
