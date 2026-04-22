package main

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
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
	Logger               *zap.Logger
	OnConnectionAcquired func()
	OnConnectionReleased func()
	OnConnectionPoolFull func()
	OnPoolUsageChanged   func(int)
}

// connectionCallbacks holds callbacks for connection lifecycle events
type connectionCallbacks struct {
	poolUsage          *atomic.Int64
	onReleased         func()
	onPoolUsageChanged func(int)
}

// ConnectionHandler is the interface for handling TCP connections
type ConnectionHandler interface {
	HandleConnection(ctx context.Context, conn net.Conn)
}

// StartTCPServer starts a TCP server with connection pooling and graceful shutdown
func StartTCPServer(ctx context.Context, wg *sync.WaitGroup, config TCPServerConfig, handler ConnectionHandler) {
	defer wg.Done()

	connSemaphore := make(chan struct{}, MaxConcurrentConnections)
	var activeConnWg sync.WaitGroup
	var poolUsage atomic.Int64

	lc := net.ListenConfig{
		KeepAlive: KeepAliveTimeout,
	}

	listener, err := lc.Listen(ctx, "tcp", config.Addr)
	if err != nil {
		config.Logger.Error("Failed to create listener",
			zap.String("addr", config.Addr), zap.Error(err))
		return
	}
	defer listener.Close()

	// Graceful shutdown handler
	go func() {
		<-ctx.Done()
		config.Logger.Info("Shutting down server...",
			zap.String("addr", config.Addr))
		listener.Close()
		activeConnWg.Wait()
		config.Logger.Info("All connections closed",
			zap.String("addr", config.Addr))
	}()

	config.Logger.Info("Server started",
		zap.String("addr", config.Addr))

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			config.Logger.Error("Accept failed",
				zap.String("addr", config.Addr), zap.Error(err))
			continue
		}

		select {
		case connSemaphore <- struct{}{}:
			activeConnWg.Add(1)
			if config.OnConnectionAcquired != nil {
				config.OnConnectionAcquired()
			}
			cb := &connectionCallbacks{
				poolUsage:          &poolUsage,
				onReleased:         config.OnConnectionReleased,
				onPoolUsageChanged: config.OnPoolUsageChanged,
			}
			go handleTCPConnection(ctx, conn, handler, connSemaphore, &activeConnWg, cb)
		default:
			config.Logger.Warn("Connection pool full, rejecting connection",
				zap.String("addr", config.Addr))
			if config.OnConnectionPoolFull != nil {
				config.OnConnectionPoolFull()
			}
			conn.Close()
		}
	}
}

// handleTCPConnection manages a single TCP connection with proper cleanup
func handleTCPConnection(ctx context.Context, conn net.Conn, handler ConnectionHandler, semaphore chan struct{}, wg *sync.WaitGroup, cb *connectionCallbacks) {
	// Increment pool usage first, then defer the decrement to ensure symmetry
	if cb.onPoolUsageChanged != nil {
		cb.onPoolUsageChanged(int(cb.poolUsage.Add(1)))
	}
	defer func() {
		conn.Close()
		<-semaphore
		if cb.onPoolUsageChanged != nil {
			cb.onPoolUsageChanged(int(cb.poolUsage.Add(-1)))
		}
		if cb.onReleased != nil {
			cb.onReleased()
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

	handler.HandleConnection(ctx, conn)
}
