package ui

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSpinnerLifecycle(t *testing.T) {
	var buf bytes.Buffer
	sp := NewSpinner(&buf, "Testing spinner")
	sp.SetForceTTY(true)

	sp.Start()
	time.Sleep(120 * time.Millisecond)
	sp.Stop()

	out := buf.String()
	if !strings.Contains(out, "Testing spinner") {
		t.Errorf("expected spinner output to contain message, got: %q", out)
	}
	if !strings.HasSuffix(out, "\r\033[K") {
		t.Errorf("expected spinner to finish with line clearing escape sequence, got: %q", out)
	}
}

func TestSpinnerNonTerminalIgnored(t *testing.T) {
	var buf bytes.Buffer
	sp := NewSpinner(&buf, "Non-terminal spinner")
	// force is false by default, and bytes.Buffer is not a terminal

	sp.Start()
	time.Sleep(50 * time.Millisecond)
	sp.Stop()

	if buf.Len() != 0 {
		t.Errorf("expected no output for non-terminal, got: %q", buf.String())
	}
}

func TestSpinnerDoubleStartStop(t *testing.T) {
	var buf bytes.Buffer
	sp := NewSpinner(&buf, "Double stop")
	sp.SetForceTTY(true)

	sp.Start()
	sp.Start() // should not panic or start second goroutine
	sp.Stop()
	sp.Stop() // should not panic
}

func TestSpinnerConcurrentSafety(t *testing.T) {
	var buf bytes.Buffer
	sp := NewSpinner(&buf, "Concurrent test")
	sp.SetForceTTY(true)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			sp.Start()
		}()
		go func() {
			defer wg.Done()
			sp.Stop()
		}()
	}
	wg.Wait()
	sp.Stop()
}
