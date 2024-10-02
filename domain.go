package main

import (
	"bytes"
	"fmt"
	"net"
	"strings"
)

type Domain struct {
	userli UserliService
}

func NewDomain(userli UserliService) *Domain {
	return &Domain{userli: userli}
}

func (d *Domain) Handle(conn net.Conn) {
	defer conn.Close()
	command := make([]byte, 4096)
	_, err := conn.Read(command)
	if err != nil {
		fmt.Println("Error reading:", err.Error())
	}

	command = bytes.Trim(command, "\x00")
	parts := strings.Split(string(command), " ")
	if len(parts) < 2 || parts[0] != "get" {
		fmt.Println("Invalid command")
		_, _ = conn.Write([]byte("400 Bad Request\n"))
		return
	}

	domain := strings.TrimSuffix(parts[1], "\n")
	exists, err := d.userli.GetDomain(string(domain))
	if err != nil {
		fmt.Println("Error fetching domain:", err.Error())
		_, _ = conn.Write([]byte("400 Error fetching domain\n"))
		return
	}

	if !exists {
		_, _ = conn.Write([]byte("500 NO%20RESULT\n"))
		return
	}

	_, _ = conn.Write([]byte("200 1\n"))
}
