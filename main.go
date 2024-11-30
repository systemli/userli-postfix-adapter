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

	go StartMetricsServer(ctx, config.MetricsListenAddr)

	var wg sync.WaitGroup

	wg.Add(4)
	go StartTCPServer(ctx, &wg, config.AliasListenAddr, adapter.AliasHandler, config.AliasMaxWorkers)
	go StartTCPServer(ctx, &wg, config.DomainListenAddr, adapter.DomainHandler, config.DomainMaxWorkers)
	go StartTCPServer(ctx, &wg, config.MailboxListenAddr, adapter.MailboxHandler, config.MailboxMaxWorkers)
	go StartTCPServer(ctx, &wg, config.SendersListenAddr, adapter.SendersHandler, config.SendersMaxWorkers)

	wg.Wait()
	log.Info("All servers stopped")
}
