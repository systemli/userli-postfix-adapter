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

	userli := NewUserli(userliToken, userliBaseURL)
	alias := NewAlias(userli)
	domain := NewDomain(userli)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	wg.Add(2)
	go StartTCPServer(ctx, &wg, aliasListenAddr, alias.Handle)
	go StartTCPServer(ctx, &wg, domainListenAddr, domain.Handle)

	wg.Wait()
	fmt.Println("Servers stopped")
}
