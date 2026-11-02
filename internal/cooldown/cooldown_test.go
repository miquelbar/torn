package cooldown

import (
	"testing"
	"time"

	"torn/internal/storage"
)

func TestCooldownTransitions(t *testing.T) {
	mgr := NewManager()
	now := time.Now()
	state := &storage.State{
		ActiveCLI: "claude",
		Cooldowns: make(map[string]time.Time),
	}

	// 1. In use / active check
	status, _ := mgr.GetStatus("claude", state, now)
	if status != StatusActive {
		t.Errorf("expected StatusActive for claude, got %v", status)
	}

	// 2. Ready check
	status, _ = mgr.GetStatus("codex", state, now)
	if status != StatusReady {
		t.Errorf("expected StatusReady for codex, got %v", status)
	}

	// 3. Set cooldown
	mgr.SetCooldown("codex", state, 2*time.Hour, now)
	status, remaining := mgr.GetStatus("codex", state, now)
	if status != StatusCooldown {
		t.Errorf("expected StatusCooldown for codex, got %v", status)
	}
	if remaining <= 0 {
		t.Errorf("expected remaining > 0, got %v", remaining)
	}

	// 4. Cooldown expired
	future := now.Add(3 * time.Hour)
	status, _ = mgr.GetStatus("codex", state, future)
	if status != StatusReady {
		t.Errorf("expected StatusReady after cooldown expired, got %v", status)
	}

	// 5. Clear cooldown
	mgr.ClearCooldown("codex", state)
	status, _ = mgr.GetStatus("codex", state, now)
	if status != StatusReady {
		t.Errorf("expected StatusReady after manual clear, got %v", status)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{d: 0, want: "0m"},
		{d: 45 * time.Minute, want: "45m"},
		{d: 1*time.Hour + 30*time.Minute, want: "1h 30m"},
		{d: 5*time.Hour + 5*time.Minute, want: "5h 05m"},
	}

	for _, tt := range tests {
		got := FormatDuration(tt.d)
		if got != tt.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
