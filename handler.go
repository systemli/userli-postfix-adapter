package main

import "net"

// PostfixHandler is an interface that defines the Handle method
// for handling postfix postmap requests.
// See https://www.postfix.org/postmap.1.html
type PostfixHandler interface {
	Handle(conn net.Conn)
}
