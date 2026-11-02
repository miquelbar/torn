package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New initializes and returns a structured slog.Logger.
func New(levelStr string, out io.Writer) *slog.Logger {
	if out == nil {
		out = os.Stdout
	}

	var level slog.Level
	switch strings.ToLower(strings.TrimSpace(levelStr)) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewTextHandler(out, &slog.HandlerOptions{
		Level: level,
	})

	return slog.New(handler)
}
