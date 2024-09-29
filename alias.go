package main

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"strings"
)

type Alias struct {
	address string
	userli  UserliService
}

func NewAlias(address string, userli UserliService) *Alias {
	return &Alias{address: address, userli: userli}
}

func (a *Alias) Listen() {
	listen, err := net.Listen("tcp", a.address)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}

	defer listen.Close()
	fmt.Println("Alias service listening on port", a.address)

	for {
		conn, err := listen.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}

		go a.handle(conn)
	}
}

func (a *Alias) handle(conn net.Conn) {
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

	email := strings.TrimSuffix(parts[1], "\n")
	aliases, err := a.userli.GetAliases(string(email))
	if err != nil {
		fmt.Println("Error fetching aliases:", err.Error())
		_, _ = conn.Write([]byte("400 Error fetching aliases\n"))
		return
	}

	if len(aliases) == 0 {
		_, _ = conn.Write([]byte("500 NO%20RESULT\n"))
		return
	}

	_, _ = conn.Write([]byte(fmt.Sprintf("200 %s \n", strings.Join(aliases, ","))))
}
