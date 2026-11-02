package config

import (
	"os"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid, got: %v", err)
	}

	if cfg.AppName != "torn" {
		t.Errorf("expected AppName to be 'torn', got %q", cfg.AppName)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected Port to be 8080, got %d", cfg.Port)
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	os.Setenv("TORN_ENV", "production")
	os.Setenv("TORN_PORT", "9090")
	os.Setenv("TORN_SHUTDOWN_TIMEOUT", "10s")
	os.Setenv("TORN_LOG_LEVEL", "debug")
	defer func() {
		os.Unsetenv("TORN_ENV")
		os.Unsetenv("TORN_PORT")
		os.Unsetenv("TORN_SHUTDOWN_TIMEOUT")
		os.Unsetenv("TORN_LOG_LEVEL")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected successful load, got: %v", err)
	}

	if cfg.Environment != "production" {
		t.Errorf("expected Environment 'production', got %q", cfg.Environment)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected Port 9090, got %d", cfg.Port)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected ShutdownTimeout 10s, got %v", cfg.ShutdownTimeout)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected LogLevel 'debug', got %q", cfg.LogLevel)
	}
}

func TestLoadConfigInvalidPort(t *testing.T) {
	os.Setenv("TORN_PORT", "invalid")
	defer os.Unsetenv("TORN_PORT")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error on invalid port, got nil")
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid",
			mutate:  func(c *Config) {},
			wantErr: false,
		},
		{
			name: "empty app name",
			mutate: func(c *Config) {
				c.AppName = ""
			},
			wantErr: true,
		},
		{
			name: "invalid port zero",
			mutate: func(c *Config) {
				c.Port = 0
			},
			wantErr: true,
		},
		{
			name: "invalid port negative",
			mutate: func(c *Config) {
				c.Port = -1
			},
			wantErr: true,
		},
		{
			name: "invalid port too high",
			mutate: func(c *Config) {
				c.Port = 70000
			},
			wantErr: true,
		},
		{
			name: "zero shutdown timeout",
			mutate: func(c *Config) {
				c.ShutdownTimeout = 0
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
