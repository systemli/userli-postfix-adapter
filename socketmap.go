package main

import (
	"context"
	"net"
	"sync"
)

// socketmapConnectionHandler wraps SocketmapAdapter to implement ConnectionHandler
type socketmapConnectionHandler struct {
	adapter *SocketmapAdapter
}

func (h *socketmapConnectionHandler) HandleConnection(conn net.Conn) {
	h.adapter.HandleConnection(conn)
}

func StartSocketmapServer(ctx context.Context, wg *sync.WaitGroup, addr string, adapter *SocketmapAdapter) {
	config := TCPServerConfig{
		Name: "socketmap",
		Addr: addr,
		OnConnectionAcquired: func() {
			activeConnections.Inc()
		},
		OnConnectionReleased: func() {
			activeConnections.Dec()
		},
	}

	handler := &socketmapConnectionHandler{adapter: adapter}
	StartTCPServer(ctx, wg, config, handler)
}
