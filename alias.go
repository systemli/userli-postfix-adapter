package main

import (
	"bytes"
	"fmt"
	"net"
	"strings"
)

type Alias struct {
	userli UserliService
}

func NewAlias(userli UserliService) *Alias {
	return &Alias{userli: userli}
}

func (a *Alias) Handle(conn net.Conn) {
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
