package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"go.uber.org/zap"
)

// TLSProber checks whether a domain's mail servers advertise STARTTLS.
// It connects on port 25, sends EHLO, and closes immediately — no mail is sent.
type TLSProber struct {
	timeout      time.Duration
	ehloHostname string
	// smtpPort is the port used for outgoing SMTP probes. Defaults to "25".
	// Overridable in tests to avoid requiring a real port 25.
	smtpPort string
	// mxLookup resolves MX records. Defaults to net.LookupMX.
	// Overridable in tests to avoid real DNS queries.
	mxLookup func(string) ([]*net.MX, error)
	logger   *zap.Logger
}

// NewTLSProber creates a new TLSProber.
func NewTLSProber(timeout time.Duration, ehloHostname string, logger *zap.Logger) *TLSProber {
	return &TLSProber{
		timeout:      timeout,
		ehloHostname: ehloHostname,
		smtpPort:     "25",
		mxLookup:     net.LookupMX,
		logger:       logger,
	}
}

// Probe returns true when at least one MX server for domain advertises STARTTLS.
// It tries up to 2 MX hosts (lowest preference first); falls back to a direct A/AAAA
// lookup when no MX records are published.
// An error is returned only when no host could be contacted at all (transient failures).
// (false, nil) means the domain was reachable but STARTTLS is not offered.
func (p *TLSProber) Probe(ctx context.Context, domain string) (bool, error) {
	hosts, err := p.resolveMXHosts(domain)
	if err != nil {
		return false, err
	}

	var lastErr error
	contacted := 0
	for _, host := range hosts {
		hasTLS, err := p.probeHost(ctx, host)
		if err != nil {
			p.logger.Debug("SMTP probe error",
				zap.String("domain", domain),
				zap.String("host", host),
				zap.Error(err))
			lastErr = err
			continue
		}
		contacted++
		if hasTLS {
			return true, nil
		}
	}

	if contacted == 0 {
		return false, fmt.Errorf("could not contact any MX for %s: %w", domain, lastErr)
	}
	return false, nil
}

// resolveMXHosts returns up to 2 MX hostnames for domain sorted by preference.
// Falls back to the domain name itself when no MX records are found.
func (p *TLSProber) resolveMXHosts(domain string) ([]string, error) {
	records, err := p.mxLookup(domain)
	if err != nil {
		if dnsErr, ok := err.(*net.DNSError); ok && dnsErr.IsNotFound {
			return []string{domain}, nil
		}
		return nil, fmt.Errorf("MX lookup %s: %w", domain, err)
	}
	if len(records) == 0 {
		return []string{domain}, nil
	}
	limit := min(len(records), 2)
	hosts := make([]string, limit)
	for i := range limit {
		hosts[i] = strings.TrimSuffix(records[i].Host, ".")
	}
	return hosts, nil
}

// probeHost opens a plain TCP connection to host:smtpPort and checks for STARTTLS.
func (p *TLSProber) probeHost(ctx context.Context, host string) (bool, error) {
	return p.probeAddr(ctx, net.JoinHostPort(host, p.smtpPort))
}

// probeAddr opens a TCP connection to addr, reads the SMTP banner, sends EHLO,
// and returns whether STARTTLS appears in the advertised extensions.
// No mail is sent; the connection is closed immediately after the check.
func (p *TLSProber) probeAddr(ctx context.Context, addr string) (bool, error) {
	dialCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(p.timeout))

	r := bufio.NewReader(conn)

	if err := skipSMTPResponse(r); err != nil {
		return false, fmt.Errorf("banner: %w", err)
	}

	if _, err := fmt.Fprintf(conn, "EHLO %s\r\n", p.ehloHostname); err != nil {
		return false, err
	}

	hasTLS, err := readEHLOExtensions(r)
	if err != nil {
		return false, fmt.Errorf("EHLO: %w", err)
	}

	_, _ = fmt.Fprintf(conn, "QUIT\r\n")
	return hasTLS, nil
}

// skipSMTPResponse reads and discards a complete SMTP response (single or multi-line).
func skipSMTPResponse(r *bufio.Reader) error {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return err
		}
		// A line with a space at position 3 is the last line of an SMTP response.
		if len(line) >= 4 && line[3] == ' ' {
			return nil
		}
	}
}

// readEHLOExtensions reads a 250 multi-line EHLO response and returns true if
// STARTTLS is listed as an advertised extension.
func readEHLOExtensions(r *bufio.Reader) (bool, error) {
	hasTLS := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return false, err
		}
		if len(line) < 4 {
			return false, fmt.Errorf("short SMTP line: %q", line)
		}
		if line[:3] != "250" {
			return false, fmt.Errorf("unexpected SMTP code in EHLO response: %.3s", line)
		}
		if strings.EqualFold(strings.TrimSpace(line[4:]), "STARTTLS") {
			hasTLS = true
		}
		if line[3] == ' ' {
			return hasTLS, nil
		}
	}
}
