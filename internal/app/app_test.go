package app

import (
	"bytes"
	"context"
	"testing"
	"time"

	"torn/internal/config"
	"torn/internal/logger"
)

func TestAppLifecycle(t *testing.T) {
	cfg := config.Default()
	var buf bytes.Buffer
	log := logger.New("info", &buf)

	application := New(cfg, log)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- application.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start returned unexpected error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("application did not shut down in time")
	}
}

func TestAppExplicitStop(t *testing.T) {
	cfg := config.Default()
	var buf bytes.Buffer
	log := logger.New("info", &buf)

	application := New(cfg, log)

	// Stop before start should return error
	if err := application.Stop(); err == nil {
		t.Fatal("expected error when stopping an unstarted app, got nil")
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- application.Start(context.Background())
	}()

	time.Sleep(50 * time.Millisecond)

	if err := application.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("application did not terminate after Stop()")
	}
}
