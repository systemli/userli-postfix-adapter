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
	StatusOK       Status = 200
	StatusError    Status = 400
	StatusNoResult Status = 500

	ResponseNoResult     string = "NO RESULT"
	ResponsePayloadError string = "PAYLOAD ERROR"

	ErrPayloadError string = "Error getting payload"
	ErrAPIError     string = "Error fetching data"
)

// Status is the status code for the response.
type Status int

// Response is the response to a postfix command.
type Response struct {
	Status   Status
	Response string
}

// String returns the response as a string.
func (r *Response) String() string {
	return fmt.Sprintf("%d %s\n", r.Status, strings.ReplaceAll(r.Response, " ", "%20"))
}

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
	defer func() {
		if err := conn.Close(); err != nil {
			log.WithError(err).Error("Error closing connection")
		}
	}()
	now := time.Now()

	payload, err := p.payload(conn)
	if err != nil {
		log.WithError(err).Error(ErrPayloadError)
		p.write(conn, Response{Status: StatusError, Response: ResponsePayloadError}, now, "alias")
		return
	}
	aliases, err := p.client.GetAliases(payload)
	if err != nil {
		log.WithError(err).WithField("email", payload).Error(ErrAPIError)
		p.write(conn, Response{Status: StatusError, Response: "Error fetching aliases"}, now, "alias")
		return
	}

	if len(aliases) == 0 {
		p.write(conn, Response{Status: StatusNoResult, Response: ResponseNoResult}, now, "alias")
		return
	}

	p.write(conn, Response{Status: StatusOK, Response: strings.Join(aliases, ",")}, now, "alias")
}

// DomainHandler handles the get command for domains.
// It checks if the domain exists.
// The response is a single line with the status code.
func (p *PostfixAdapter) DomainHandler(conn net.Conn) {
	defer func() {
		if err := conn.Close(); err != nil {
			log.WithError(err).Error("Error closing connection")
		}
	}()
	now := time.Now()

	payload, err := p.payload(conn)
	if err != nil {
		log.WithError(err).Error("Error getting payload")
		p.write(conn, Response{Status: StatusError, Response: ResponsePayloadError}, now, "domain")
		return
	}

	exists, err := p.client.GetDomain(payload)
	if err != nil {
		log.WithError(err).WithField("domain", payload).Error(ErrAPIError)
		p.write(conn, Response{Status: StatusError, Response: "Error fetching domain"}, now, "domain")
		return
	}

	if !exists {
		p.write(conn, Response{Status: StatusNoResult, Response: ResponseNoResult}, now, "domain")
		return
	}

	p.write(conn, Response{Status: StatusOK, Response: "1"}, now, "domain")
}

// MailboxHandler handles the get command for mailboxes.
// It checks if the mailbox exists.
// The response is a single line with the status code.
func (p *PostfixAdapter) MailboxHandler(conn net.Conn) {
	defer func() {
		if err := conn.Close(); err != nil {
			log.WithError(err).Error("Error closing connection")
		}
	}()
	now := time.Now()

	payload, err := p.payload(conn)
	if err != nil {
		log.WithError(err).Error(ErrPayloadError)
		p.write(conn, Response{Status: StatusError, Response: ResponsePayloadError}, now, "mailbox")
		return
	}

	exists, err := p.client.GetMailbox(payload)
	if err != nil {
		log.WithError(err).WithField("email", payload).Error(ErrAPIError)
		p.write(conn, Response{Status: StatusError, Response: "Error fetching mailbox"}, now, "mailbox")
		return
	}

	if !exists {
		p.write(conn, Response{Status: StatusNoResult, Response: ResponseNoResult}, now, "mailbox")
		return
	}

	p.write(conn, Response{Status: StatusOK, Response: "1"}, now, "mailbox")
}

// SendersHandler handles the get command for senders.
// It fetches the senders for the given email.
// The response is a comma separated list of senders.
func (p *PostfixAdapter) SendersHandler(conn net.Conn) {
	defer func() {
		if err := conn.Close(); err != nil {
			log.WithError(err).Error("Error closing connection")
		}
	}()
	now := time.Now()

	payload, err := p.payload(conn)
	if err != nil {
		log.WithError(err).Error(ErrPayloadError)
		p.write(conn, Response{Status: StatusError, Response: ResponsePayloadError}, now, "senders")
		return
	}

	senders, err := p.client.GetSenders(payload)
	if err != nil {
		log.WithError(err).WithField("email", payload).Error(ErrAPIError)
		p.write(conn, Response{Status: StatusError, Response: "Error fetching senders"}, now, "senders")
		return
	}

	if len(senders) == 0 {
		p.write(conn, Response{Status: StatusNoResult, Response: ResponseNoResult}, now, "senders")
		return
	}

	p.write(conn, Response{Status: StatusOK, Response: strings.Join(senders, ",")}, now, "senders")
}

// payload reads the data from the connection. It checks for valid
// commands sent by postfix and returns the payload.
func (h *PostfixAdapter) payload(conn net.Conn) (string, error) {
	_ = conn.SetReadDeadline(time.Now().Add(ReadTimeout))

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

func (h *PostfixAdapter) write(conn net.Conn, response Response, now time.Time, handler string) {
	var status string
	switch response.Status {
	case StatusOK:
		status = "success"
	default:
		status = "error"
	}

	log.WithFields(log.Fields{"response": response.String(), "handler": handler, "status": status}).Debug("Writing response")

	_ = conn.SetWriteDeadline(time.Now().Add(WriteTimeout))

	_, err := conn.Write([]byte(response.String()))
	if err != nil {
		log.WithError(err).WithFields(log.Fields{"response": response.String(), "handler": handler, "status": status}).Error("Error writing response")
	}
	requestDurations.With(prometheus.Labels{"handler": handler, "status": status}).Observe(time.Since(now).Seconds())
}
