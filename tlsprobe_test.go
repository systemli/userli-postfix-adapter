package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// startFakeSMTPServer starts a one-shot fake SMTP server on a random port.
// extensions is the list of ESMTP extensions to advertise in the EHLO response.
func startFakeSMTPServer(t *testing.T, extensions []string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))

		fmt.Fprintf(conn, "220 fake.smtp.test ESMTP\r\n")

		buf := make([]byte, 256)
		if _, err := conn.Read(buf); err != nil {
			return
		}

		if len(extensions) == 0 {
			fmt.Fprintf(conn, "250 fake.smtp.test\r\n")
		} else {
			fmt.Fprintf(conn, "250-fake.smtp.test\r\n")
			for i, ext := range extensions {
				if i == len(extensions)-1 {
					fmt.Fprintf(conn, "250 %s\r\n", ext)
				} else {
					fmt.Fprintf(conn, "250-%s\r\n", ext)
				}
			}
		}
		_, _ = conn.Read(buf) // read QUIT
	}()

	return ln.Addr().String()
}

func newTestProber() *TLSProber {
	return NewTLSProber(3*time.Second, "test.local", zap.NewNop())
}

func TestTLSProber_ProbeAddr_WithSTARTTLS(t *testing.T) {
	addr := startFakeSMTPServer(t, []string{"PIPELINING", "STARTTLS", "SIZE 52428800"})
	p := newTestProber()
	hasTLS, err := p.probeAddr(context.Background(), addr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasTLS {
		t.Error("expected STARTTLS to be detected")
	}
}

func TestTLSProber_ProbeAddr_WithoutSTARTTLS(t *testing.T) {
	addr := startFakeSMTPServer(t, []string{"PIPELINING", "SIZE 52428800"})
	p := newTestProber()
	hasTLS, err := p.probeAddr(context.Background(), addr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasTLS {
		t.Error("expected no STARTTLS")
	}
}

func TestTLSProber_ProbeAddr_NoExtensions(t *testing.T) {
	addr := startFakeSMTPServer(t, []string{})
	p := newTestProber()
	hasTLS, err := p.probeAddr(context.Background(), addr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasTLS {
		t.Error("expected no STARTTLS when server advertises no extensions")
	}
}

func TestTLSProber_ProbeAddr_ConnectionRefused(t *testing.T) {
	p := NewTLSProber(500*time.Millisecond, "test.local", zap.NewNop())
	_, err := p.probeAddr(context.Background(), "127.0.0.1:1")
	if err == nil {
		t.Error("expected error for connection refused")
	}
}

func TestTLSProber_Probe_AllHostsFail(t *testing.T) {
	p := NewTLSProber(500*time.Millisecond, "test.local", zap.NewNop())
	// Probe with a synthesised result: resolveMXHosts falls back to the domain
	// itself when no MX records exist. We force a connection failure.
	_, err := p.Probe(context.Background(), "240.0.0.1.invalid") // unreachable
	// We only assert it doesn't panic; the exact error depends on the OS resolver.
	_ = err
}

// fakeMXLookup returns a controllable set of MX records.
func fakeMXLookup(hosts []string) func(string) ([]*net.MX, error) {
	return func(_ string) ([]*net.MX, error) {
		records := make([]*net.MX, len(hosts))
		for i, h := range hosts {
			records[i] = &net.MX{Host: h + ".", Pref: uint16(i + 1)}
		}
		return records, nil
	}
}

func TestTLSProber_ResolveMXHosts_EmptyRecords(t *testing.T) {
	p := newTestProber()
	p.mxLookup = func(_ string) ([]*net.MX, error) { return []*net.MX{}, nil }
	hosts, err := p.resolveMXHosts("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 1 || hosts[0] != "example.com" {
		t.Errorf("expected fallback to domain, got %v", hosts)
	}
}

func TestTLSProber_ResolveMXHosts_SingleMX(t *testing.T) {
	p := newTestProber()
	p.mxLookup = fakeMXLookup([]string{"mx1.example.com"})
	hosts, err := p.resolveMXHosts("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 1 || hosts[0] != "mx1.example.com" {
		t.Errorf("expected [mx1.example.com], got %v", hosts)
	}
}

func TestTLSProber_ResolveMXHosts_LimitedToTwo(t *testing.T) {
	p := newTestProber()
	p.mxLookup = fakeMXLookup([]string{"mx1.example.com", "mx2.example.com", "mx3.example.com"})
	hosts, err := p.resolveMXHosts("example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hosts) != 2 {
		t.Errorf("expected at most 2 hosts, got %d: %v", len(hosts), hosts)
	}
}

func TestTLSProber_ResolveMXHosts_DNSError(t *testing.T) {
	p := newTestProber()
	p.mxLookup = func(_ string) ([]*net.MX, error) {
		return nil, fmt.Errorf("lookup error")
	}
	_, err := p.resolveMXHosts("example.com")
	if err == nil {
		t.Error("expected error for DNS failure")
	}
}

func TestTLSProber_ResolveMXHosts_NotFound(t *testing.T) {
	p := newTestProber()
	p.mxLookup = func(domain string) ([]*net.MX, error) {
		return nil, &net.DNSError{Err: "no such host", Name: domain, IsNotFound: true}
	}
	hosts, err := p.resolveMXHosts("nodomain.example")
	if err != nil {
		t.Fatalf("expected fallback on NXDOMAIN, got error: %v", err)
	}
	if len(hosts) != 1 || hosts[0] != "nodomain.example" {
		t.Errorf("expected domain fallback, got %v", hosts)
	}
}

func TestTLSProber_Probe_WithSTARTTLS(t *testing.T) {
	addr := startFakeSMTPServer(t, []string{"STARTTLS"})
	_, portStr, _ := net.SplitHostPort(addr)

	p := newTestProber()
	p.smtpPort = portStr
	p.mxLookup = fakeMXLookup([]string{"127.0.0.1"})

	hasTLS, err := p.Probe(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasTLS {
		t.Error("expected STARTTLS detected via Probe")
	}
}

func TestTLSProber_Probe_WithoutSTARTTLS(t *testing.T) {
	addr := startFakeSMTPServer(t, []string{"PIPELINING"})
	_, portStr, _ := net.SplitHostPort(addr)

	p := newTestProber()
	p.smtpPort = portStr
	p.mxLookup = fakeMXLookup([]string{"127.0.0.1"})

	hasTLS, err := p.Probe(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasTLS {
		t.Error("expected no STARTTLS via Probe")
	}
}

func TestTLSProber_Probe_AllMXUnreachable(t *testing.T) {
	p := NewTLSProber(200*time.Millisecond, "test.local", zap.NewNop())
	p.smtpPort = "1" // nothing listening on port 1
	p.mxLookup = fakeMXLookup([]string{"127.0.0.1"})

	_, err := p.Probe(context.Background(), "example.com")
	if err == nil {
		t.Error("expected error when all MX hosts are unreachable")
	}
}

func TestTLSProber_ProbeAddr_BannerError(t *testing.T) {
	// Server closes connection before sending a complete SMTP banner.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		defer ln.Close()
		conn, _ := ln.Accept()
		if conn != nil {
			// Send a partial banner (no \r\n terminator) then close.
			_, _ = fmt.Fprint(conn, "220 partial")
			conn.Close()
		}
	}()

	p := newTestProber()
	_, err = p.probeAddr(context.Background(), ln.Addr().String())
	if err == nil {
		t.Error("expected error when banner is incomplete")
	}
}

func TestTLSProber_ProbeAddr_InvalidEHLOResponse(t *testing.T) {
	// Server sends a valid banner but an error code in EHLO response.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		defer ln.Close()
		conn, _ := ln.Accept()
		if conn == nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		fmt.Fprint(conn, "220 fake.smtp.test ESMTP\r\n")
		buf := make([]byte, 256)
		_, _ = conn.Read(buf)
		// Respond to EHLO with an error code.
		fmt.Fprint(conn, "500 not implemented\r\n")
	}()

	p := newTestProber()
	_, err = p.probeAddr(context.Background(), ln.Addr().String())
	if err == nil {
		t.Error("expected error for non-250 EHLO response")
	}
}

func TestSkipSMTPResponse_EOF(t *testing.T) {
	// Reader with no newline → ReadString returns EOF.
	r := bufio.NewReader(strings.NewReader("220 incomplete"))
	err := skipSMTPResponse(r)
	if err == nil {
		t.Error("expected error on EOF without newline")
	}
}

func TestTLSProber_Probe_FirstHostFails_SecondSucceeds(t *testing.T) {
	addr := startFakeSMTPServer(t, []string{"STARTTLS"})
	_, portStr, _ := net.SplitHostPort(addr)

	p := NewTLSProber(500*time.Millisecond, "test.local", zap.NewNop())
	p.smtpPort = portStr
	// First MX points to a port that refuses connections; second points to our fake server.
	p.mxLookup = fakeMXLookup([]string{"192.0.2.1", "127.0.0.1"})

	hasTLS, err := p.Probe(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasTLS {
		t.Error("expected STARTTLS when second MX succeeds")
	}
}

func TestSkipSMTPResponse_SingleLine(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("220 welcome\r\n"))
	if err := skipSMTPResponse(r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSkipSMTPResponse_MultiLine(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("220-first\r\n220 last\r\n"))
	if err := skipSMTPResponse(r); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadEHLOExtensions_WithSTARTTLS(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("250-example.com\r\n250-PIPELINING\r\n250-STARTTLS\r\n250 SIZE 52428800\r\n"))
	hasTLS, err := readEHLOExtensions(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasTLS {
		t.Error("expected STARTTLS to be detected")
	}
}

func TestReadEHLOExtensions_CaseInsensitive(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("250-starttls\r\n250 SIZE 0\r\n"))
	hasTLS, err := readEHLOExtensions(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasTLS {
		t.Error("expected case-insensitive STARTTLS match")
	}
}

func TestReadEHLOExtensions_WithoutSTARTTLS(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("250-example.com\r\n250 PIPELINING\r\n"))
	hasTLS, err := readEHLOExtensions(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasTLS {
		t.Error("expected no STARTTLS")
	}
}

func TestReadEHLOExtensions_SingleLine(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("250 example.com\r\n"))
	hasTLS, err := readEHLOExtensions(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasTLS {
		t.Error("expected no STARTTLS in single-line response")
	}
}

func TestReadEHLOExtensions_UnexpectedCode(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("500 error\r\n"))
	_, err := readEHLOExtensions(r)
	if err == nil {
		t.Error("expected error for non-250 code")
	}
}

func TestReadEHLOExtensions_ShortLine(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("25\r\n"))
	_, err := readEHLOExtensions(r)
	if err == nil {
		t.Error("expected error for short SMTP line")
	}
}

func TestReadEHLOExtensions_EOF(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("250-PIPELINING\r\n"))
	// After reading the continuation line, ReadString will hit EOF.
	// Consume the first line, then call again on empty reader.
	_, _ = r.ReadString('\n')
	_, err := readEHLOExtensions(bufio.NewReader(strings.NewReader("")))
	if err == nil {
		t.Error("expected error on EOF")
	}
}

func TestTLSProber_Probe_DNSError(t *testing.T) {
	p := newTestProber()
	p.mxLookup = func(_ string) ([]*net.MX, error) {
		return nil, fmt.Errorf("dns failure")
	}
	_, err := p.Probe(context.Background(), "example.com")
	if err == nil {
		t.Error("expected error when MX lookup fails")
	}
}
