package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"torn/internal/config"
)

// App manages the application lifecycle and dependencies.
type App struct {
	cfg    *config.Config
	logger *slog.Logger
	mu     sync.Mutex
	cancel context.CancelFunc
}

// New creates a new instance of App.
func New(cfg *config.Config, logger *slog.Logger) *App {
	return &App{
		cfg:    cfg,
		logger: logger,
	}
}

// Start begins the application background routines.
func (a *App) Start(ctx context.Context) error {
	a.mu.Lock()
	appCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel
	a.mu.Unlock()

	a.logger.Info("starting application",
		"app", a.cfg.AppName,
		"env", a.cfg.Environment,
		"port", a.cfg.Port,
	)

	<-appCtx.Done()
	a.logger.Info("shutting down application")
	return nil
}

// Stop initiates a graceful shutdown.
func (a *App) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.cancel != nil {
		a.cancel()
		return nil
	}
	return fmt.Errorf("application is not running")
}
