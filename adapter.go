package main

import (
	"bytes"
	"fmt"
	"net"
	"strings"
)

// PostfixAdapter is an adapter for postfix postmap commands.
// See https://www.postfix.org/postmap.1.html
type PostfixAdapter struct {
	client UserliService
}

// NewPostfixAdapter creates a new Handler with the given UserliService.
func NewPostfixAdapter(client UserliService) *PostfixAdapter {
	return &PostfixAdapter{client: client}
}

// AliasHandler handles the get command for aliases.
// It fetches the destinations for the given alias.
// The response is a comma separated list of destinations.
func (p *PostfixAdapter) AliasHandler(conn net.Conn) {
	defer conn.Close()

	payload, err := p.payload(conn)
	if err != nil {
		fmt.Println("Error getting payload:", err.Error())
		_, _ = conn.Write([]byte("400 Error getting payload\n"))
		return
	}
	email := strings.TrimSuffix(payload, "\n")
	aliases, err := p.client.GetAliases(string(email))
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

// DomainHandler handles the get command for domains.
// It checks if the domain exists.
// The response is a single line with the status code.
func (p *PostfixAdapter) DomainHandler(conn net.Conn) {
	defer conn.Close()

	payload, err := p.payload(conn)
	if err != nil {
		fmt.Println("Error getting payload:", err.Error())
		_, _ = conn.Write([]byte("400 Error getting payload\n"))
		return
	}

	domain := strings.TrimSuffix(payload, "\n")
	exists, err := p.client.GetDomain(string(domain))
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

// MailboxHandler handles the get command for mailboxes.
// It checks if the mailbox exists.
// The response is a single line with the status code.
func (p *PostfixAdapter) MailboxHandler(conn net.Conn) {
	defer conn.Close()

	payload, err := p.payload(conn)
	if err != nil {
		fmt.Println("Error getting payload:", err.Error())
		_, _ = conn.Write([]byte("400 Error getting payload\n"))
		return
	}

	email := strings.TrimSuffix(payload, "\n")
	exists, err := p.client.GetMailbox(string(email))
	if err != nil {
		fmt.Println("Error fetching mailbox:", err.Error())
		_, _ = conn.Write([]byte("400 Error fetching mailbox\n"))
		return
	}

	if !exists {
		_, _ = conn.Write([]byte("500 NO%20RESULT\n"))
		return
	}

	_, _ = conn.Write([]byte("200 1\n"))
}

// SendersHandler handles the get command for senders.
// It fetches the senders for the given email.
// The response is a comma separated list of senders.
func (p *PostfixAdapter) SendersHandler(conn net.Conn) {
	defer conn.Close()

	payload, err := p.payload(conn)
	if err != nil {
		fmt.Println("Error getting payload:", err.Error())
		_, _ = conn.Write([]byte("400 Error getting payload\n"))
		return
	}

	email := strings.TrimSuffix(payload, "\n")
	senders, err := p.client.GetSenders(string(email))
	if err != nil {
		fmt.Println("Error fetching senders:", err.Error())
		_, _ = conn.Write([]byte("400 Error fetching senders\n"))
		return
	}

	if len(senders) == 0 {
		_, _ = conn.Write([]byte("500 NO%20RESULT\n"))
		return
	}

	_, _ = conn.Write([]byte(fmt.Sprintf("200 %s \n", strings.Join(senders, ","))))
}

// payload reads the data from the connection. It checks for valid
// commands sent by postfix and returns the payload.
func (h *PostfixAdapter) payload(conn net.Conn) (string, error) {
	data := make([]byte, 4096)
	_, err := conn.Read(data)
	if err != nil {
		return "", err
	}

	data = bytes.Trim(data, "\x00")
	parts := strings.Split(string(data), " ")
	if len(parts) < 2 || parts[0] != "get" {
		return "", fmt.Errorf("invalid command")
	}

	return parts[1], nil
}
