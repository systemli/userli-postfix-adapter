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
	socketmapAdapter := NewSocketmapAdapter(userli)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go StartMetricsServer(ctx, config.MetricsListenAddr)

	var wg sync.WaitGroup

	wg.Add(1)
	go StartSocketmapServer(ctx, &wg, config.SocketmapListenAddr, socketmapAdapter)

	wg.Wait()
	log.Info("Socketmap server stopped")
}
