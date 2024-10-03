package main

import (
	"context"
	"os/signal"
	"sync"
	"syscall"

	log "github.com/sirupsen/logrus"
)

func main() {
	config := NewConfig()
	userli := NewUserli(config.UserliToken, config.UserliBaseURL)
	adapter := NewPostfixAdapter(userli)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup

	wg.Add(4)
	go StartTCPServer(ctx, &wg, config.AliasListenAddr, adapter.AliasHandler)
	go StartTCPServer(ctx, &wg, config.DomainListenAddr, adapter.DomainHandler)
	go StartTCPServer(ctx, &wg, config.MailboxListenAddr, adapter.MailboxHandler)
	go StartTCPServer(ctx, &wg, config.SendersListenAddr, adapter.SendersHandler)

	wg.Wait()
	log.Info("All servers stopped")
}
