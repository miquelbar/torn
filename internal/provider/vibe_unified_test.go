package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeVibeUnifiedMeta(t *testing.T, sessionDir, sessionID, cwd, model, bumpedAt string) {
	t.Helper()
	meta := map[string]any{
		"session_id": sessionID,
		"start_time": bumpedAt,
		"end_time":   bumpedAt,
		"bumped_at":  bumpedAt,
		"environment": map[string]any{
			"working_directory": cwd,
		},
		"config": map[string]any{
			"active_model": model,
			"model_name":   "zai-" + model,
		},
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "meta.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

func writeVibeProjectionState(t *testing.T, sessionDir, generation string, usage vibeTokenUsage) {
	t.Helper()
	genDir := filepath.Join(sessionDir, "generations", generation)
	if err := os.MkdirAll(genDir, 0755); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"snapshot": map[string]any{
			"session": map[string]any{
				"tokenUsage": map[string]any{
					"inputTokens":       usage.InputTokens,
					"cachedInputTokens": usage.CachedInputTokens,
					"outputTokens":      usage.OutputTokens,
					"totalTokens":       usage.TotalTokens,
				},
			},
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "projection-state.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
}

// appendVibeEntryEvent builds a projection_delta event appending an entry.
func appendVibeEntryEvent(role, text string, createdAt int64) string {
	ev := map[string]any{
		"type": "projection_delta",
		"payload": map[string]any{
			"delta": []map[string]any{{
				"op": "append_entry",
				"entry": map[string]any{
					"role":      role,
					"createdAt": createdAt,
					"content":   []map[string]any{{"type": "text", "text": text}},
				},
			}},
		},
	}
	raw, _ := json.Marshal(ev)
	return string(raw)
}

// appendVibeEnvelopeEvent builds a projection_delta set_envelope event.
func appendVibeEnvelopeEvent(turnID, status string, startedAt, completedAt int64, usage vibeTokenUsage) string {
	envelope := map[string]any{
		"session": map[string]any{
			"tokenUsage": map[string]any{
				"inputTokens":       usage.InputTokens,
				"cachedInputTokens": usage.CachedInputTokens,
				"outputTokens":      usage.OutputTokens,
				"totalTokens":       usage.TotalTokens,
			},
		},
	}
	if turnID != "" {
		turn := map[string]any{
			"id":        turnID,
			"status":    status,
			"startedAt": startedAt,
		}
		if completedAt > 0 {
			turn["completedAt"] = completedAt
		}
		envelope["latestTurn"] = turn
	}
	ev := map[string]any{
		"type": "projection_delta",
		"payload": map[string]any{
			"delta": []map[string]any{{
				"op":    "set_envelope",
				"state": envelope,
			}},
		},
	}
	raw, _ := json.Marshal(ev)
	return string(raw)
}

func writeVibeJournal(t *testing.T, sessionDir string, lines ...string) {
	t.Helper()
	journalDir := filepath.Join(sessionDir, "journal")
	if err := os.MkdirAll(journalDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(journalDir, "0000000000000001.jsonl"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestVibeUnifiedDetectParseAndUsage(t *testing.T) {
	tempDir := t.TempDir()
	p := NewVibe(tempDir)
	cwd := "/Users/test/Projects/hemdemenjar"
	now := time.Now().Format(time.RFC3339)

	sessionDir := filepath.Join(tempDir, "logs", "session", "unified", "11111111-2222-3333-4444-555555555555")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeVibeUnifiedMeta(t, sessionDir, "11111111-2222-3333-4444-555555555555", cwd, "glm-5-3", now)
	writeVibeProjectionState(t, sessionDir, "0000000000000005", vibeTokenUsage{
		InputTokens: 1000, CachedInputTokens: 800, OutputTokens: 100, TotalTokens: 1100,
	})

	startedAt := time.Now().Add(-4 * time.Second).UnixMilli()
	completedAt := time.Now().UnixMilli()
	writeVibeJournal(t, sessionDir,
		appendVibeEntryEvent("user", "acaba el formulario", startedAt-1000),
		appendVibeEnvelopeEvent("turn-1", "in_progress", startedAt, 0, vibeTokenUsage{InputTokens: 900, CachedInputTokens: 800}),
		appendVibeEntryEvent("assistant", "formulario listo y commiteado", completedAt),
		appendVibeEnvelopeEvent("turn-1", "completed", startedAt, completedAt, vibeTokenUsage{InputTokens: 1000, CachedInputTokens: 800, OutputTokens: 100}),
	)

	// 1. DetectLatestSession finds the unified session.
	sess, err := p.DetectLatestSession(cwd)
	if err != nil {
		t.Fatalf("DetectLatestSession failed: %v", err)
	}
	if sess.ID != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("expected unified session id, got %q", sess.ID)
	}
	if !IsVibeUnifiedSession(sess.Path) {
		t.Errorf("session path %q not detected as unified", sess.Path)
	}

	// 2. DetectLatestSession ignores sessions from other projects.
	other, err := p.DetectLatestSession("/Users/test/Projects/other")
	if err == nil || other != nil {
		t.Errorf("expected no session for other project, got %+v", other)
	}

	// 3. ParseSession extracts the conversation from the journal.
	details, err := p.ParseSession(sess.Path)
	if err != nil {
		t.Fatalf("ParseSession failed: %v", err)
	}
	if details.LastUserMessage != "acaba el formulario" {
		t.Errorf("unexpected user message: %q", details.LastUserMessage)
	}
	if !strings.Contains(details.LastAssistantMessage, "commiteado") {
		t.Errorf("unexpected assistant message: %q", details.LastAssistantMessage)
	}
	if len(details.ModelsUsed) != 1 || details.ModelsUsed[0] != "glm-5-3" {
		t.Errorf("unexpected models: %v", details.ModelsUsed)
	}
	// (1000 - 800) non-cached + 100 completion = 300
	if details.TotalTokens != 300 {
		t.Errorf("expected 300 total tokens, got %d", details.TotalTokens)
	}

	// 4. CollectUsage aggregates sessions, tokens, and speed.
	metrics, err := p.CollectUsage(time.Now(), cwd)
	if err != nil {
		t.Fatalf("CollectUsage failed: %v", err)
	}
	if metrics.TotalSessions != 1 {
		t.Errorf("expected 1 session, got %d", metrics.TotalSessions)
	}
	if metrics.TotalTokens != 300 {
		t.Errorf("expected 300 total tokens, got %d", metrics.TotalTokens)
	}
	if metrics.TokensToday != 300 {
		t.Errorf("expected 300 tokens today, got %d", metrics.TokensToday)
	}
	if metrics.LastModel != "glm-5-3" {
		t.Errorf("unexpected last model: %q", metrics.LastModel)
	}

	ms, ok := metrics.ModelStats["glm-5-3"]
	if !ok {
		t.Fatalf("missing model stats for glm-5-3: %v", metrics.ModelStats)
	}
	if ms.Turns != 1 {
		t.Errorf("expected 1 turn, got %d", ms.Turns)
	}
	if ms.TokensPerSec <= 0 {
		t.Errorf("expected positive tokens/sec, got %f (out=%d secs=%f)", ms.TokensPerSec, ms.OutputTokens, ms.DurationSecs)
	}
	if metrics.TokensPerSec <= 0 {
		t.Errorf("expected positive provider tokens/sec, got %f", metrics.TokensPerSec)
	}
}

func TestVibeUnifiedUsageFallsBackToJournalEnvelope(t *testing.T) {
	tempDir := t.TempDir()
	p := NewVibe(tempDir)
	cwd := "/Users/test/Projects/fallback-app"
	now := time.Now()

	sessionDir := filepath.Join(tempDir, "logs", "session", "unified", "aaaa1111-2222-3333-4444-555555555555")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	// No generations yet: active session mid-turn.
	writeVibeUnifiedMeta(t, sessionDir, "aaaa1111-2222-3333-4444-555555555555", cwd, "mistral-medium-3.5", now.Format(time.RFC3339))
	writeVibeJournal(t, sessionDir,
		appendVibeEnvelopeEvent("turn-9", "in_progress", now.UnixMilli(), 0, vibeTokenUsage{InputTokens: 500, CachedInputTokens: 400, OutputTokens: 40, TotalTokens: 540}),
	)

	metrics, err := p.CollectUsage(time.Now(), cwd)
	if err != nil {
		t.Fatalf("CollectUsage failed: %v", err)
	}
	if metrics.TotalSessions != 1 {
		t.Errorf("expected 1 session, got %d", metrics.TotalSessions)
	}
	// (500 - 400) + 40 = 140
	if metrics.TotalTokens != 140 {
		t.Errorf("expected 140 total tokens, got %d", metrics.TotalTokens)
	}
}

func TestVibeUnifiedMergesWithLegacyStore(t *testing.T) {
	tempDir := t.TempDir()
	p := NewVibe(tempDir)
	cwd := "/Users/test/Projects/merge-app"
	now := time.Now()

	// Legacy session with stats.
	legacyDir := filepath.Join(tempDir, "logs", "session", "session_20260101_100000_legacy001")
	if err := os.MkdirAll(legacyDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacyMeta := map[string]any{
		"session_id": "legacy-uuid",
		"start_time": "2026-01-01T10:00:00Z",
		"stats": map[string]any{
			"session_prompt_tokens":     1000,
			"session_cached_tokens":     900,
			"session_completion_tokens": 50,
			"steps":                     2,
		},
		"config": map[string]any{"active_model": "glm-5-3"},
	}
	data, _ := json.Marshal(legacyMeta)
	if err := os.WriteFile(filepath.Join(legacyDir, "meta.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	index := map[string]vibeIndexEntry{
		"session_20260101_100000_legacy001": {
			SessionID: "legacy-uuid",
			CWD:       cwd,
			UpdatedAt: "2026-01-01T10:05:00Z",
		},
	}
	indexData, _ := json.Marshal(index)
	if err := os.WriteFile(filepath.Join(tempDir, "logs", "session", ".session_index.json"), indexData, 0644); err != nil {
		t.Fatal(err)
	}
	// The legacy collector falls back to the session dir mtime as its activity
	// time; make the legacy session look old.
	oldTime := time.Now().AddDate(0, 0, -30)
	if err := os.Chtimes(legacyDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	// Unified session for the same project.
	unifiedDir := filepath.Join(tempDir, "logs", "session", "unified", "bbbb1111-2222-3333-4444-555555555555")
	if err := os.MkdirAll(unifiedDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeVibeUnifiedMeta(t, unifiedDir, "bbbb1111-2222-3333-4444-555555555555", cwd, "glm-5-3", now.Format(time.RFC3339))
	writeVibeProjectionState(t, unifiedDir, "0000000000000001", vibeTokenUsage{
		InputTokens: 200, CachedInputTokens: 200, OutputTokens: 60, TotalTokens: 260,
	})

	metrics, err := p.CollectUsage(time.Now(), cwd)
	if err != nil {
		t.Fatalf("CollectUsage failed: %v", err)
	}
	if metrics.TotalSessions != 2 {
		t.Errorf("expected 2 sessions (legacy + unified), got %d", metrics.TotalSessions)
	}
	// Legacy: (1000-900)+50 = 150. Unified: (200-200)+60 = 60.
	if metrics.TotalTokens != 210 {
		t.Errorf("expected 210 total tokens, got %d", metrics.TotalTokens)
	}
	if metrics.TokensToday != 60 {
		t.Errorf("expected 60 tokens today (unified only), got %d", metrics.TokensToday)
	}
}
