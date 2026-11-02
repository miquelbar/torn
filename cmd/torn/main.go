package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"torn/internal/app"
	"torn/internal/config"
	"torn/internal/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	log := logger.New(cfg.LogLevel, os.Stdout)

	application := app.New(cfg, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return application.Start(ctx)
}
