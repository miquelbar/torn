package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"torn/internal/project"
)

func TestStoreStateLifecycle(t *testing.T) {
	tempBase := t.TempDir()
	store, err := New(tempBase)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	info := &project.Info{
		Path: "/test/project",
		Hash: "abcdef0123456789",
	}

	// 1. Initial load should return empty defaults
	state, err := store.LoadState(info)
	if err != nil {
		t.Fatalf("initial LoadState failed: %v", err)
	}
	if state.ActiveCLI != "" {
		t.Errorf("expected empty ActiveCLI, got %q", state.ActiveCLI)
	}

	// 2. Mutate and save state
	now := time.Now().Truncate(time.Second)
	state.ActiveCLI = "claude"
	state.ActiveSessionID = "session-123"
	state.LastGitHead = "head-hash-abc"
	state.Cooldowns["codex"] = now.Add(2 * time.Hour)

	if err := store.SaveState(info, state); err != nil {
		t.Fatalf("SaveState failed: %v", err)
	}

	// 3. Reload state and verify
	loaded, err := store.LoadState(info)
	if err != nil {
		t.Fatalf("reloaded LoadState failed: %v", err)
	}

	if loaded.ActiveCLI != "claude" {
		t.Errorf("expected ActiveCLI 'claude', got %q", loaded.ActiveCLI)
	}
	if loaded.ActiveSessionID != "session-123" {
		t.Errorf("expected ActiveSessionID 'session-123', got %q", loaded.ActiveSessionID)
	}
	if loaded.LastGitHead != "head-hash-abc" {
		t.Errorf("expected LastGitHead 'head-hash-abc', got %q", loaded.LastGitHead)
	}
	if !loaded.Cooldowns["codex"].Equal(now.Add(2 * time.Hour)) {
		t.Errorf("expected cooldown timestamp to match, got %v", loaded.Cooldowns["codex"])
	}
}

func TestStoreHistoryAppend(t *testing.T) {
	tempBase := t.TempDir()
	store, err := New(tempBase)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	info := &project.Info{
		Path: "/test/project",
		Hash: "1234567890abcdef",
	}

	entry1 := &HistoryEntry{
		Timestamp:   time.Now(),
		FromCLI:     "claude",
		ToCLI:       "codex",
		SessionID:   "s-1",
		GitHead:     "g-1",
		TurnsPassed: 10,
	}

	entry2 := &HistoryEntry{
		Timestamp:   time.Now(),
		FromCLI:     "codex",
		ToCLI:       "vibe",
		SessionID:   "s-2",
		GitHead:     "g-2",
		TurnsPassed: 5,
		Dropped:     2,
	}

	if err := store.AppendHistory(info, entry1); err != nil {
		t.Fatalf("first AppendHistory failed: %v", err)
	}
	if err := store.AppendHistory(info, entry2); err != nil {
		t.Fatalf("second AppendHistory failed: %v", err)
	}

	historyFile := filepath.Join(tempBase, info.Hash, "history.jsonl")
	data, err := os.ReadFile(historyFile)
	if err != nil {
		t.Fatalf("failed reading history file: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines in history.jsonl, got %d", len(lines))
	}
}
