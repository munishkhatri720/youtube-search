package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx := context.Background()

	shutdownCtx, shutdownCancel := signal.NotifyContext(
		ctx,
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGINT,
	)

	defer shutdownCancel()

	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	flag.Parse()

	if configPath == nil || *configPath == "" {
		panic(errors.New("config path is required"))
	}

	cfg, err := ReadConfig(*configPath)
	if err != nil {
		panic(fmt.Errorf("failed to read config: %w", err))
	}

	slog.Info("Configuration loaded", "config", cfg.String())

	SetupLogger(cfg.Logging)

	server := &Server{Cfg: cfg}
	server.client = NewHttpClient(cfg.RequestTimeout, cfg.Ipv6Subnet)

	server.Start(shutdownCtx)
	slog.Info("Server started", "address", cfg.ServerAddr)

	if cfg.Caching.Enabled {
		if err := server.ConnectDb(shutdownCtx); err != nil {
			slog.Error("Failed to connect to database", "error", err)
			panic(err)
		}
	}

	server.visitors = make([]*YouTubeVisitorData, 0)
	server.ticker = time.NewTicker(30 * time.Minute)

	for i := 0; i < cfg.MaxVisitorCount; i++ {
		visitor, err := server.fetchInnertubeContext(ctx)
		if err != nil {
			slog.Error("Failed to fetch visitor data", "error", err)
		} else {
			slog.Info("Fetched new visitor data", slog.Any("visitor", visitor.VisitorID()))
			server.visitors = append(server.visitors, visitor)
		}
	}

	go server.RotateVisitors(shutdownCtx)

	slog.Info("Press Ctrl+C to shut down the server")

	<-shutdownCtx.Done()

	if server.db != nil {
		if err := server.db.Close(); err != nil {
			slog.Error("Error closing database", "error", err)
		}
	}

	slog.Info("Shutting down server...")
	if err := server.Stop(ctx); err != nil {
		slog.Error("Error shutting down server", "error", err)
	} else {
		slog.Info("Server shut down gracefully")
	}

}
