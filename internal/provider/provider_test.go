package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRegistry(t *testing.T) {
	reg := DefaultRegistry()

	all := reg.All()
	if len(all) < 4 {
		t.Fatalf("expected at least 4 providers, got %d", len(all))
	}

	claude, err := reg.Get("claude")
	if err != nil {
		t.Fatalf("failed to get claude: %v", err)
	}
	if claude.Name() != "claude" {
		t.Errorf("expected claude, got %s", claude.Name())
	}

	next := reg.NextAvailable("claude")
	if next == nil || next.Name() != "codex" {
		t.Errorf("expected codex after claude, got %v", next)
	}

	_, err = reg.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent provider, got nil")
	}
}

func TestClaudeProvider(t *testing.T) {
	tempDir := t.TempDir()
	p := NewClaude(tempDir)

	cwd := "/Users/test/Projects/my-app"
	slug := p.PathToSlug(cwd)
	projectDir := filepath.Join(tempDir, "projects", slug)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	sessionFile := filepath.Join(projectDir, "sess-1.jsonl")
	content := `{"role": "user", "content": "build a feature"}
{"role": "assistant", "content": "feature built successfully"}
`
	if err := os.WriteFile(sessionFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	sess, err := p.DetectLatestSession(cwd)
	if err != nil {
		t.Fatalf("DetectLatestSession failed: %v", err)
	}
	if sess.ID != "sess-1" {
		t.Errorf("expected sess-1, got %s", sess.ID)
	}

	details, err := p.ParseSession(sess.Path)
	if err != nil {
		t.Fatalf("ParseSession failed: %v", err)
	}
	if len(details.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(details.Turns))
	}
	if details.LastUserMessage != "build a feature" {
		t.Errorf("expected user message, got %q", details.LastUserMessage)
	}
	if details.LastAssistantMessage != "feature built successfully" {
		t.Errorf("expected assistant message, got %q", details.LastAssistantMessage)
	}
}

func TestCodexProvider(t *testing.T) {
	tempDir := t.TempDir()
	p := NewCodex(tempDir)

	targetCwd := "/Users/test/Projects/target-app"
	sessionsDir := filepath.Join(tempDir, "sessions", "2026", "11", "02")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Session for different CWD
	otherFile := filepath.Join(sessionsDir, "rollout-other.jsonl")
	otherContent := `{"type": "session_meta", "payload": {"cwd": "/other/path", "session_id": "other-id"}}`
	_ = os.WriteFile(otherFile, []byte(otherContent), 0644)

	// Session for target CWD
	targetFile := filepath.Join(sessionsDir, "rollout-target.jsonl")
	targetContent := `{"type": "session_meta", "payload": {"cwd": "/Users/test/Projects/target-app", "session_id": "target-id"}}
{"type": "user_message", "payload": {"role": "user", "text": "implement algorithm"}}
{"type": "assistant_message", "payload": {"role": "assistant", "text": "algorithm done"}}
`
	_ = os.WriteFile(targetFile, []byte(targetContent), 0644)

	sess, err := p.DetectLatestSession(targetCwd)
	if err != nil {
		t.Fatalf("DetectLatestSession failed: %v", err)
	}
	if sess.ID != "target-id" {
		t.Errorf("expected target-id, got %s", sess.ID)
	}

	details, err := p.ParseSession(sess.Path)
	if err != nil {
		t.Fatalf("ParseSession failed: %v", err)
	}
	if len(details.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(details.Turns))
	}
}

func TestVibeProvider(t *testing.T) {
	tempDir := t.TempDir()
	p := NewVibe(tempDir)

	logsDir := filepath.Join(tempDir, "logs", "session")
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		t.Fatal(err)
	}

	index := map[string]vibeIndexEntry{
		"session_1": {
			SessionID: "vibe-sess-1",
			CWD:       "/Users/test/Projects/vibe-app",
			UpdatedAt: time.Now().Format(time.RFC3339),
		},
	}
	data, _ := json.Marshal(index)
	_ = os.WriteFile(filepath.Join(logsDir, ".session_index.json"), data, 0644)

	sess, err := p.DetectLatestSession("/Users/test/Projects/vibe-app")
	if err != nil {
		t.Fatalf("DetectLatestSession failed: %v", err)
	}
	if sess.ID != "vibe-sess-1" {
		t.Errorf("expected vibe-sess-1, got %s", sess.ID)
	}
}
