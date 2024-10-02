package main

import (
	"context"
	"fmt"
	"net"
	"sync"
)

func StartTCPServer(ctx context.Context, wg *sync.WaitGroup, addr string, handler func(net.Conn)) {
	defer wg.Done()

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Println("Error creating listener:", err.Error())
		return
	}
	defer listener.Close()

	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				fmt.Println("Server stopped on port ", addr)
				return
			}
			fmt.Println("Error accepting connection:", err.Error())
			continue
		}

		go handler(conn)
	}
}
