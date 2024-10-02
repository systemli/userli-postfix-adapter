package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

var (
	userliToken     string
	userliBaseURL   string
	aliasListenAddr string
)

func main() {
	userliBaseURL = os.Getenv("USERLI_BASE_URL")
	if userliBaseURL == "" {
		userliBaseURL = "http://localhost:8000"
	}

	userliToken = os.Getenv("USERLI_TOKEN")
	if userliToken == "" {
		fmt.Println("USERLI_TOKEN is required")
		os.Exit(1)
	}

	aliasListenAddr = os.Getenv("ALIAS_LISTEN_ADDR")
	if aliasListenAddr == "" {
		aliasListenAddr = ":10001"
	}

	domainListenAddr := os.Getenv("DOMAIN_LISTEN_ADDR")
	if domainListenAddr == "" {
		domainListenAddr = ":10002"
	}

	mailboxListenAddr := os.Getenv("MAILBOX_LISTEN_ADDR")
	if mailboxListenAddr == "" {
		mailboxListenAddr = ":10003"
	}

	sendersListenAddr := os.Getenv("SENDERS_LISTEN_ADDR")
	if sendersListenAddr == "" {
		sendersListenAddr = ":10004"
	}

	userli := NewUserli(userliToken, userliBaseURL)
	adapter := NewPostfixAdapter(userli)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	wg.Add(4)
	go StartTCPServer(ctx, &wg, aliasListenAddr, adapter.AliasHandler)
	go StartTCPServer(ctx, &wg, domainListenAddr, adapter.DomainHandler)
	go StartTCPServer(ctx, &wg, mailboxListenAddr, adapter.MailboxHandler)
	go StartTCPServer(ctx, &wg, sendersListenAddr, adapter.SendersHandler)

	wg.Wait()
	fmt.Println("Servers stopped")
}
