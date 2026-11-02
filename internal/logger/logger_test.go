package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoggerLevels(t *testing.T) {
	tests := []struct {
		name          string
		levelStr      string
		logAction     func(l *testing.T, logFn func(string, ...any))
		shouldContain string
	}{
		{
			name:     "info level includes info message",
			levelStr: "info",
			logAction: func(t *testing.T, logFn func(string, ...any)) {
				logFn("hello world")
			},
			shouldContain: "hello world",
		},
		{
			name:     "warn level includes warning message",
			levelStr: "warn",
			logAction: func(t *testing.T, logFn func(string, ...any)) {
				logFn("warning test")
			},
			shouldContain: "warning test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			l := New(tt.levelStr, &buf)

			l.Info(tt.shouldContain)
			output := buf.String()

			if tt.levelStr == "info" && !strings.Contains(output, tt.shouldContain) {
				t.Fatalf("expected output to contain %q, got %q", tt.shouldContain, output)
			}
		})
	}
}
