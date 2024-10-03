package main

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

const (
	ErrPayloadError = "Error getting payload"
	ErrAPIError     = "Error fetching data"

	ResponseNoResult     = "500 NO%20RESULT\n"
	ResponsePayloadError = "500 PAYLOAD%20ERROR\n"
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

	now := time.Now()

	payload, err := p.payload(conn)
	if err != nil {
		log.WithError(err).Error(ErrPayloadError)
		p.write(conn, []byte(ResponsePayloadError), now, "alias", "error")
		return
	}
	aliases, err := p.client.GetAliases(payload)
	if err != nil {
		log.WithError(err).WithField("email", payload).Error(ErrAPIError)
		p.write(conn, []byte("400 Error fetching aliases\n"), now, "alias", "error")
		return
	}

	if len(aliases) == 0 {
		p.write(conn, []byte(ResponseNoResult), now, "alias", "success")
		return
	}

	p.write(conn, []byte(fmt.Sprintf("200 %s \n", strings.Join(aliases, ","))), now, "alias", "success")
}

// DomainHandler handles the get command for domains.
// It checks if the domain exists.
// The response is a single line with the status code.
func (p *PostfixAdapter) DomainHandler(conn net.Conn) {
	defer conn.Close()

	now := time.Now()

	payload, err := p.payload(conn)
	if err != nil {
		log.WithError(err).Error("Error getting payload")
		p.write(conn, []byte(ResponsePayloadError), now, "domain", "error")
		return
	}

	exists, err := p.client.GetDomain(payload)
	if err != nil {
		log.WithError(err).WithField("domain", payload).Error(ErrAPIError)
		p.write(conn, []byte("400 Error fetching domain\n"), now, "domain", "error")
		return
	}

	if !exists {
		p.write(conn, []byte(ResponseNoResult), now, "domain", "success")
		return
	}

	p.write(conn, []byte("200 1\n"), now, "domain", "success")
}

// MailboxHandler handles the get command for mailboxes.
// It checks if the mailbox exists.
// The response is a single line with the status code.
func (p *PostfixAdapter) MailboxHandler(conn net.Conn) {
	defer conn.Close()

	now := time.Now()

	payload, err := p.payload(conn)
	if err != nil {
		log.WithError(err).Error(ErrPayloadError)
		p.write(conn, []byte(ResponsePayloadError), now, "mailbox", "error")
		return
	}

	exists, err := p.client.GetMailbox(payload)
	if err != nil {
		log.WithError(err).WithField("email", payload).Error(ErrAPIError)
		p.write(conn, []byte("400 Error fetching mailbox\n"), now, "mailbox", "error")
		return
	}

	if !exists {
		p.write(conn, []byte(ResponseNoResult), now, "mailbox", "success")
		return
	}

	p.write(conn, []byte("200 1\n"), now, "mailbox", "success")
}

// SendersHandler handles the get command for senders.
// It fetches the senders for the given email.
// The response is a comma separated list of senders.
func (p *PostfixAdapter) SendersHandler(conn net.Conn) {
	defer conn.Close()

	now := time.Now()

	payload, err := p.payload(conn)
	if err != nil {
		log.WithError(err).Error(ErrPayloadError)
		p.write(conn, []byte(ResponsePayloadError), now, "senders", "error")
		return
	}

	senders, err := p.client.GetSenders(payload)
	if err != nil {
		log.WithError(err).WithField("email", payload).Error(ErrAPIError)
		p.write(conn, []byte("400 Error fetching senders\n"), now, "senders", "error")
		return
	}

	if len(senders) == 0 {
		p.write(conn, []byte(ResponseNoResult), now, "senders", "success")
		return
	}

	p.write(conn, []byte(fmt.Sprintf("200 %s \n", strings.Join(senders, ","))), now, "senders", "success")
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
		return "", errors.New("invalid or unsupported command")
	}

	payload := strings.TrimSuffix(parts[1], "\n")

	log.WithFields(log.Fields{"command": parts[0], "payload": payload}).Debug("Received payload")

	return payload, nil
}

func (h *PostfixAdapter) write(conn net.Conn, response []byte, now time.Time, handler, status string) {
	log.WithFields(log.Fields{"response": string(response), "handler": handler, "status": status}).Debug("Writing response")

	_, err := conn.Write(response)
	if err != nil {
		log.WithError(err).WithFields(log.Fields{"response": string(response), "handler": handler, "status": status}).Error("Error writing response")
	}
	requestDurations.With(prometheus.Labels{"handler": handler, "status": status}).Observe(time.Since(now).Seconds())
}
