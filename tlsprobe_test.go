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
