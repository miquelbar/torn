package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config defines the runtime configuration for torn.
type Config struct {
	AppName         string        `json:"app_name"`
	Environment     string        `json:"environment"`
	Port            int           `json:"port"`
	ShutdownTimeout time.Duration `json:"shutdown_timeout"`
	LogLevel        string        `json:"log_level"`
}

// Default returns a Config populated with sensible defaults.
func Default() *Config {
	return &Config{
		AppName:         "torn",
		Environment:     "development",
		Port:            8080,
		ShutdownTimeout: 5 * time.Second,
		LogLevel:        "info",
	}
}

// Load loads configuration from environment variables, falling back to defaults.
func Load() (*Config, error) {
	cfg := Default()

	if env := os.Getenv("TORN_ENV"); env != "" {
		cfg.Environment = env
	}

	if portStr := os.Getenv("TORN_PORT"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TORN_PORT %q: %w", portStr, err)
		}
		cfg.Port = port
	}

	if timeoutStr := os.Getenv("TORN_SHUTDOWN_TIMEOUT"); timeoutStr != "" {
		timeout, err := time.ParseDuration(timeoutStr)
		if err != nil {
			return nil, fmt.Errorf("invalid TORN_SHUTDOWN_TIMEOUT %q: %w", timeoutStr, err)
		}
		cfg.ShutdownTimeout = timeout
	}

	if level := os.Getenv("TORN_LOG_LEVEL"); level != "" {
		cfg.LogLevel = level
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Validate checks that configuration values satisfy domain constraints.
func (c *Config) Validate() error {
	if c.AppName == "" {
		return fmt.Errorf("app_name cannot be empty")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", c.Port)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown_timeout must be positive")
	}
	return nil
}
