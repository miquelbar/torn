package provider

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestClaudePolymorphicParser(t *testing.T) {
	tempDir := t.TempDir()
	p := NewClaude(tempDir)

	cwd := "/Users/test/Projects/my-app"
	slug := p.PathToSlug(cwd)
	projectDir := filepath.Join(tempDir, "projects", slug)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Real-world Claude JSONL with polymorphic content blocks, tool_use, and token usage
	sessionFile := filepath.Join(projectDir, "session-real.jsonl")
	content := `{"type":"mode","mode":"normal","sessionId":"session-real"}
{"parentUuid":null,"type":"user","message":{"role":"user","content":"Implement the user authentication endpoint"},"timestamp":"2026-11-02T10:00:00Z"}
{"parentUuid":"1","type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Checking codebase first..."},{"type":"text","text":"I will inspect the router before writing the endpoint."},{"type":"tool_use","name":"Bash","input":{"command":"git status && ls -la internal/"}}],"usage":{"input_tokens":1200,"output_tokens":250,"cache_read_input_tokens":5000,"cache_creation_input_tokens":0}},"timestamp":"2026-11-02T10:00:05Z"}
{"parentUuid":"2","type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Authentication handler implemented successfully with unit tests."}],"usage":{"input_tokens":1500,"output_tokens":300}},"timestamp":"2026-11-02T10:01:00Z"}
`
	if err := os.WriteFile(sessionFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	sess, err := p.DetectLatestSession(cwd)
	if err != nil {
		t.Fatalf("DetectLatestSession failed: %v", err)
	}
	if sess.ID != "session-real" {
		t.Errorf("expected session-real, got %s", sess.ID)
	}

	details, err := p.ParseSession(sess.Path)
	if err != nil {
		t.Fatalf("ParseSession failed: %v", err)
	}

	if details.LastUserMessage != "Implement the user authentication endpoint" {
		t.Errorf("unexpected LastUserMessage: %q", details.LastUserMessage)
	}
	if !strings.Contains(details.LastAssistantMessage, "Authentication handler implemented") {
		t.Errorf("unexpected LastAssistantMessage: %q", details.LastAssistantMessage)
	}
	if details.TotalTokens <= 0 {
		t.Errorf("expected TotalTokens > 0, got %d", details.TotalTokens)
	}

	// Check tool extraction
	foundBash := false
	for _, trn := range details.Turns {
		for _, tool := range trn.Tools {
			if strings.Contains(tool, "Bash") {
				foundBash = true
			}
		}
	}
	if !foundBash {
		t.Errorf("expected tool_use 'Bash' to be extracted, turns: %+v", details.Turns)
	}
}

func TestCodexPolymorphicParser(t *testing.T) {
	tempDir := t.TempDir()
	p := NewCodex(tempDir)

	targetCwd := "/Users/test/Projects/codex-app"
	sessionsDir := filepath.Join(tempDir, "sessions", "2026", "11", "02")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}

	targetFile := filepath.Join(sessionsDir, "rollout-target.jsonl")
	// Real-world Codex structure with polymorphic blocks
	targetContent := `{"type":"session_meta","payload":{"cwd":"/Users/test/Projects/codex-app","session_id":"codex-sess-uuid"}}
{"type":"turn_context","payload":{"cwd":"/Users/test/Projects/codex-app"}}
{"type":"response_item","payload":{"role":"user","content":[{"type":"input_text","text":"Refactor the database queries for performance"}]}}
{"type":"response_item","payload":{"role":"assistant","content":[{"type":"output_text","text":"Analyzed slow query logs and added composite index on (tenant_id, created_at)."}]}}
{"type":"event_msg","payload":{"type":"item_completed","item":{"type":"CommandExecution","command":["make","test"]}}}
`
	if err := os.WriteFile(targetFile, []byte(targetContent), 0644); err != nil {
		t.Fatal(err)
	}

	sess, err := p.DetectLatestSession(targetCwd)
	if err != nil {
		t.Fatalf("DetectLatestSession failed: %v", err)
	}
	if sess.ID != "codex-sess-uuid" {
		t.Errorf("expected codex-sess-uuid, got %s", sess.ID)
	}

	details, err := p.ParseSession(sess.Path)
	if err != nil {
		t.Fatalf("ParseSession failed: %v", err)
	}

	if details.LastUserMessage != "Refactor the database queries for performance" {
		t.Errorf("unexpected user message: %q", details.LastUserMessage)
	}
	if !strings.Contains(details.LastAssistantMessage, "composite index") {
		t.Errorf("unexpected assistant message: %q", details.LastAssistantMessage)
	}
}

func TestVibeMessagesParser(t *testing.T) {
	tempDir := t.TempDir()
	p := NewVibe(tempDir)

	sessionDirName := "session_20261102_100000_abcd"
	sessionPath := filepath.Join(tempDir, "logs", "session", sessionDirName)
	if err := os.MkdirAll(sessionPath, 0755); err != nil {
		t.Fatal(err)
	}

	index := map[string]vibeIndexEntry{
		sessionDirName: {
			SessionID: "vibe-real-uuid",
			CWD:       "/Users/test/Projects/vibe-app",
			UpdatedAt: time.Now().Format(time.RFC3339),
		},
	}
	data, _ := json.Marshal(index)
	_ = os.WriteFile(filepath.Join(tempDir, "logs", "session", ".session_index.json"), data, 0644)

	// Real-world Vibe messages.jsonl
	messagesContent := `{"role": "user", "content": "Fix memory leak in background worker"}
{"role": "assistant", "content": "Identified unbounded channel growth, adding bounded buffer.", "tool_calls": [{"function": {"name": "read_file", "arguments": "{\"file_path\": \"worker.go\"}"}}]}
`
	_ = os.WriteFile(filepath.Join(sessionPath, "messages.jsonl"), []byte(messagesContent), 0644)

	sess, err := p.DetectLatestSession("/Users/test/Projects/vibe-app")
	if err != nil {
		t.Fatalf("DetectLatestSession failed: %v", err)
	}
	if sess.ID != "vibe-real-uuid" {
		t.Errorf("expected vibe-real-uuid, got %s", sess.ID)
	}

	details, err := p.ParseSession(sess.Path)
	if err != nil {
		t.Fatalf("ParseSession failed: %v", err)
	}
	if details.LastUserMessage != "Fix memory leak in background worker" {
		t.Errorf("unexpected user message: %q", details.LastUserMessage)
	}
	if !strings.Contains(details.LastAssistantMessage, "bounded buffer") {
		t.Errorf("unexpected assistant message: %q", details.LastAssistantMessage)
	}
}

func TestAgyTranscriptParser(t *testing.T) {
	tempDir := t.TempDir()
	p := NewAgy(tempDir)

	convID := "agy-test-conv-123"
	workspace := "/Users/test/Projects/agy-app"

	// history.jsonl with workspace field
	historyContent := `{"display":"Help me with the project","timestamp":1793649474024,"workspace":"/Users/test/Projects/agy-app","conversationId":"agy-test-conv-123"}
`
	_ = os.WriteFile(filepath.Join(tempDir, "history.jsonl"), []byte(historyContent), 0644)

	// brain transcript.jsonl
	brainLogsDir := filepath.Join(tempDir, "brain", convID, ".system_generated", "logs")
	_ = os.MkdirAll(brainLogsDir, 0755)

	transcriptContent := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","content":"<USER_REQUEST>\nImplement zero-footprint storage\n</USER_REQUEST>"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","thinking":"Designing storage with atomic writes...","input_tokens":2500,"output_tokens":400,"tool_calls":[{"name":"write_file","args":{"Path":"/Users/test/Projects/agy-app/storage.go"}}]}
`
	_ = os.WriteFile(filepath.Join(brainLogsDir, "transcript.jsonl"), []byte(transcriptContent), 0644)

	sess, err := p.DetectLatestSession(workspace)
	if err != nil {
		t.Fatalf("DetectLatestSession failed: %v", err)
	}
	if sess.ID != convID {
		t.Errorf("expected %s, got %s", convID, sess.ID)
	}

	details, err := p.ParseSession(sess.Path)
	if err != nil {
		t.Fatalf("ParseSession failed: %v", err)
	}
	if details.LastUserMessage != "Implement zero-footprint storage" {
		t.Errorf("unexpected user message: %q", details.LastUserMessage)
	}
	if !strings.Contains(details.LastAssistantMessage, "Designing storage") {
		t.Errorf("unexpected assistant message: %q", details.LastAssistantMessage)
	}
	if details.TotalTokens != 2900 {
		t.Errorf("expected 2900 total tokens, got %d", details.TotalTokens)
	}

	execArgs := p.FormatExecArgs("new prompt")
	if len(execArgs) != 3 || execArgs[0] != "agy" || execArgs[1] != "-i" || execArgs[2] != "new prompt" {
		t.Errorf("unexpected agy FormatExecArgs: %v", execArgs)
	}
}

func TestAgyToolExtractionAndMarkerCleaning(t *testing.T) {
	tempDir := t.TempDir()
	p := NewAgy(tempDir)

	workspace := "/Users/test/Projects/agy-target"
	convID := "conv-test-tools"

	// 1. Setup session directory and history.jsonl
	sessionDir := filepath.Join(tempDir, "conversations", convID)
	_ = os.MkdirAll(sessionDir, 0755)
	historyContent := fmt.Sprintf(`{"conversationId":%q,"workspace":%q,"timestamp":1793649474024}`+"\n", convID, workspace)
	_ = os.WriteFile(filepath.Join(tempDir, "history.jsonl"), []byte(historyContent), 0644)

	sessMeta := map[string]any{
		"session_id":     convID,
		"workspace_path": workspace,
		"status":         "active",
		"updated_at":     time.Now().Format(time.RFC3339),
	}
	rawMeta, _ := json.Marshal(sessMeta)
	_ = os.WriteFile(filepath.Join(sessionDir, "session.json"), rawMeta, 0644)

	brainLogsDir := filepath.Join(tempDir, "brain", convID, ".system_generated", "logs")
	_ = os.MkdirAll(brainLogsDir, 0755)

	transcriptContent := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","content":"<USER_REQUEST>\nRefactor auth logic\n<truncated 500 bytes>\n</USER_REQUEST>"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","thinking":"Examining the code <truncated 1200 bytes> carefully","input_tokens":100,"output_tokens":50,"tool_calls":[{"name":"write_to_file","args":{"TargetFile":"internal/auth/handler.go"}},{"name":"view_file","args":{"AbsolutePath":"/Users/test/Projects/agy-target/main.go"}}]}
`
	transcriptPath := filepath.Join(brainLogsDir, "transcript.jsonl")
	_ = os.WriteFile(transcriptPath, []byte(transcriptContent), 0644)

	details, err := p.ParseSession(transcriptPath)
	if err != nil {
		t.Fatalf("ParseSession failed: %v", err)
	}

	if details.LastUserMessage != "Refactor auth logic" {
		t.Errorf("expected clean user message, got %q", details.LastUserMessage)
	}

	if len(details.Turns) < 2 {
		t.Fatalf("expected at least 2 turns, got %d", len(details.Turns))
	}

	assistantTurn := details.Turns[1]
	if strings.Contains(assistantTurn.Text, "<truncated") {
		t.Errorf("expected thinking text without truncated markers, got %q", assistantTurn.Text)
	}

	expectedTools := []string{
		`write_to_file("internal/auth/handler.go")`,
		`view_file("main.go")`,
	}
	if len(assistantTurn.Tools) != len(expectedTools) {
		t.Fatalf("expected %d tools, got %d: %v", len(expectedTools), len(assistantTurn.Tools), assistantTurn.Tools)
	}
	for i, expected := range expectedTools {
		if assistantTurn.Tools[i] != expected {
			t.Errorf("tool[%d]: expected %q, got %q", i, expected, assistantTurn.Tools[i])
		}
	}
}

func TestAgyCollectUsagePerDayTokens(t *testing.T) {
	tempDir := t.TempDir()
	p := NewAgy(tempDir)

	now := time.Date(2026, 11, 3, 14, 0, 0, 0, time.UTC)
	yesterday := now.AddDate(0, 0, -1)

	convID := "conv-multi-day"
	historyContent := fmt.Sprintf(`{"conversationId":%q,"workspace":"/test/ws","timestamp":%d}`+"\n", convID, yesterday.UnixMilli())
	_ = os.WriteFile(filepath.Join(tempDir, "history.jsonl"), []byte(historyContent), 0644)

	brainLogsDir := filepath.Join(tempDir, "brain", convID, ".system_generated", "logs")
	_ = os.MkdirAll(brainLogsDir, 0755)

	transcriptContent := fmt.Sprintf(`{"step_index":0,"created_at":%q,"input_tokens":5000,"output_tokens":500}
{"step_index":1,"created_at":%q,"input_tokens":3000,"output_tokens":200}
`, yesterday.Format(time.RFC3339), now.Format(time.RFC3339))
	_ = os.WriteFile(filepath.Join(brainLogsDir, "transcript.jsonl"), []byte(transcriptContent), 0644)

	usage, err := p.CollectUsage(now)
	if err != nil {
		t.Fatalf("CollectUsage failed: %v", err)
	}

	if usage.TotalSessions != 1 {
		t.Errorf("expected 1 session, got %d", usage.TotalSessions)
	}
	if usage.TotalTokens != 8700 {
		t.Errorf("expected 8700 total tokens, got %d", usage.TotalTokens)
	}
	if usage.TokensThisWeek != 8700 {
		t.Errorf("expected 8700 tokens this week, got %d", usage.TokensThisWeek)
	}
	if usage.TokensToday != 3200 {
		t.Errorf("expected 3200 tokens today, got %d", usage.TokensToday)
	}
}

func TestExtractResetTime(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Madrid")
	now := time.Date(2026, 11, 3, 9, 0, 0, 0, loc)
	refTime := time.Date(2026, 11, 3, 8, 45, 0, 0, loc)

	// 1. Same-day reset in the future
	text := "You've hit your session limit · resets 12:30pm (Europe/Madrid) · progress saved"
	res := ExtractResetTime(text, refTime, now)
	if res == nil {
		t.Fatalf("expected reset time, got nil")
	}
	expected := time.Date(2026, 11, 3, 12, 30, 0, 0, loc)
	if !res.Equal(expected) {
		t.Errorf("expected %v, got %v", expected, res)
	}

	// 2. Next-day reset when candidate hour is before session hour (e.g., late night session)
	nightRef := time.Date(2026, 11, 3, 23, 15, 0, 0, loc)
	nightNow := time.Date(2026, 11, 3, 23, 20, 0, 0, loc)
	nightText := "resets 4am (Europe/Madrid)"
	nightRes := ExtractResetTime(nightText, nightRef, nightNow)
	if nightRes == nil {
		t.Fatalf("expected next-day reset time, got nil")
	}
	nightExpected := time.Date(2026, 11, 4, 4, 0, 0, 0, loc)
	if !nightRes.Equal(nightExpected) {
		t.Errorf("expected %v, got %v", nightExpected, nightRes)
	}

	// 3. Past reset (already expired relative to now)
	oldRef := time.Date(2026, 9, 17, 18, 0, 0, 0, loc)
	oldRes := ExtractResetTime(text, oldRef, now)
	if oldRes != nil {
		t.Errorf("expected nil for expired reset, got %v", oldRes)
	}
}

func TestMultiModelSessionSwitching(t *testing.T) {
	tempDir := t.TempDir()
	p := NewClaude(tempDir)

	cwd := "/Users/test/Projects/model-switch"
	slug := p.PathToSlug(cwd)
	projectDir := filepath.Join(tempDir, "projects", slug)
	_ = os.MkdirAll(projectDir, 0755)

	sessionFile := filepath.Join(projectDir, "session-switching.jsonl")
	// Turn 1 with Opus: delta 4.0s, 200 output tokens -> 50 tok/s
	// Turn 2 with Sonnet: delta 2.0s, 100 output tokens -> 50 tok/s
	content := `{"parentUuid":null,"type":"user","message":{"role":"user","content":"Plan architecture"},"timestamp":"2026-11-03T10:00:00Z"}
{"parentUuid":"1","type":"assistant","message":{"id":"msg1","model":"claude-opus-4-6-20251001","role":"assistant","content":[{"type":"text","text":"Planning with Opus"}],"usage":{"input_tokens":1000,"output_tokens":200}},"timestamp":"2026-11-03T10:00:04Z"}
{"parentUuid":"2","type":"user","message":{"role":"user","content":"Now implement it"},"timestamp":"2026-11-03T10:00:10Z"}
{"parentUuid":"3","type":"assistant","message":{"id":"msg2","model":"claude-sonnet-4-5-20250929","role":"assistant","content":[{"type":"text","text":"Implementing with Sonnet"}],"usage":{"input_tokens":1500,"output_tokens":100}},"timestamp":"2026-11-03T10:00:12Z"}
`
	_ = os.WriteFile(sessionFile, []byte(content), 0644)

	details, err := p.ParseSession(sessionFile)
	if err != nil {
		t.Fatalf("ParseSession failed: %v", err)
	}

	if len(details.ModelsUsed) != 2 {
		t.Fatalf("expected 2 models used, got %d: %v", len(details.ModelsUsed), details.ModelsUsed)
	}
	if details.ModelsUsed[0] != "claude-opus-4-6" || details.ModelsUsed[1] != "claude-sonnet-4-5" {
		t.Errorf("expected [claude-opus-4-6 claude-sonnet-4-5], got %v", details.ModelsUsed)
	}

	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	usage, err := p.CollectUsage(now)
	if err != nil {
		t.Fatalf("CollectUsage failed: %v", err)
	}

	if len(usage.ModelStats) != 2 {
		t.Fatalf("expected 2 model stats, got %d", len(usage.ModelStats))
	}
	opus := usage.ModelStats["claude-opus-4-6"]
	if opus.TotalTokens != 1200 {
		t.Errorf("expected 1200 opus tokens, got %d", opus.TotalTokens)
	}
	sonnet := usage.ModelStats["claude-sonnet-4-5"]
	if sonnet.TotalTokens != 1600 {
		t.Errorf("expected 1600 sonnet tokens, got %d", sonnet.TotalTokens)
	}
	if usage.TokensPerSec <= 0 {
		t.Errorf("expected positive TokensPerSec, got %f", usage.TokensPerSec)
	}

	// Test project scoped filtering
	projUsage, err := p.CollectUsage(now, cwd)
	if err != nil {
		t.Fatalf("CollectUsage with matching project failed: %v", err)
	}
	if projUsage.TotalSessions != 1 {
		t.Errorf("expected 1 session for %s, got %d", cwd, projUsage.TotalSessions)
	}

	diffUsage, err := p.CollectUsage(now, "/Users/test/Projects/other")
	if err != nil {
		t.Fatalf("CollectUsage with other project failed: %v", err)
	}
	if diffUsage.TotalSessions != 0 {
		t.Errorf("expected 0 sessions for other project, got %d", diffUsage.TotalSessions)
	}
}

func TestExtractResetTimeRelativeDurations(t *testing.T) {
	now := time.Date(2026, 11, 13, 20, 0, 0, 0, time.UTC)
	anchor := time.Date(2026, 11, 13, 19, 28, 6, 0, time.UTC)

	// Real agy error message format.
	msg := "API error (attempt 1): RESOURCE_EXHAUSTED (code 429): Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 2h15m49s."
	until := ExtractResetTime(msg, anchor, now)
	if until == nil {
		t.Fatal("expected reset time from agy quota message, got nil")
	}
	want := anchor.Add(2*time.Hour + 15*time.Minute + 49*time.Second)
	if !until.Equal(want) {
		t.Errorf("expected %v, got %v", want, until)
	}
}

func TestExtractResetTimeDurationVariants(t *testing.T) {
	now := time.Date(2026, 11, 13, 20, 0, 0, 0, time.UTC)

	cases := []struct {
		text string
		want time.Duration
	}{
		{"Resets in 4h8m33s.", 4*time.Hour + 8*time.Minute + 33*time.Second},
		{"resets in 8m10s", 8*time.Minute + 10*time.Second},
		{"resets in 45s", 45 * time.Second},
		{"Your limit resets in 3h", 3 * time.Hour},
	}
	for _, tc := range cases {
		until := ExtractResetTime(tc.text, now, now)
		if until == nil {
			t.Errorf("no reset parsed for %q", tc.text)
			continue
		}
		if got := until.Sub(now); got != tc.want {
			t.Errorf("%q: expected +%v, got +%v", tc.text, tc.want, got)
		}
	}
}

func TestDetectQuotaCooldown(t *testing.T) {
	now := time.Date(2026, 11, 13, 20, 0, 0, 0, time.UTC)
	anchor := now.Add(-10 * time.Minute)

	// Positive: exhaustion evidence + reset in the same message.
	msg := "Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 1h30m."
	if until := DetectQuotaCooldown(msg, anchor, now); until == nil {
		t.Error("expected cooldown from quota message")
	}

	// Negative: conversation text mentioning quota without reset info.
	chatter := "quota is exhausted. Do not attempt to use backup AI credits."
	if until := DetectQuotaCooldown(chatter, anchor, now); until != nil {
		t.Error("expected no cooldown for ordinary conversational text")
	}

	// Negative: reset announcement without exhaustion evidence.
	plain := "The maintenance window resets in 2h."
	if until := DetectQuotaCooldown(plain, anchor, now); until != nil {
		t.Error("expected no cooldown without exhaustion evidence")
	}

	// Negative: expired reset (quota window already passed).
	expired := "Individual quota reached. Resets in 5m."
	if until := DetectQuotaCooldown(expired, anchor.Add(-1*time.Hour), now); until != nil {
		t.Error("expected no cooldown for expired quota window")
	}
}

func TestAgyQuotaCooldownParsing(t *testing.T) {
	tempDir := t.TempDir()
	p := NewAgy(tempDir)

	convID := "agy-quota-conv-1"
	workspace := "/Users/test/Projects/agy-app"

	historyContent := fmt.Sprintf(`{"display":"help","timestamp":1793649474024,"workspace":%q,"conversationId":%q}`, workspace, convID)
	if err := os.WriteFile(filepath.Join(tempDir, "history.jsonl"), []byte(historyContent), 0644); err != nil {
		t.Fatal(err)
	}

	transcriptDir := filepath.Join(tempDir, "brain", convID, ".system_generated", "logs")
	if err := os.MkdirAll(transcriptDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Real agy quota error step (RESOURCE_EXHAUSTED) in transcript format.
	quotaStep := `{"step_index":1780,"source":"SYSTEM","type":"ERROR_MESSAGE","status":"DONE","error":"API error (attempt 1): RESOURCE_EXHAUSTED (code 429): Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 4h8m33s.","created_at":"2026-11-13T19:36:00Z"}` + "\n"
	plainStep := `{"type":"USER_INPUT","content":"hello","created_at":"2026-11-13T19:35:00Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(transcriptDir, "transcript.jsonl"), []byte(plainStep+quotaStep), 0644); err != nil {
		t.Fatal(err)
	}

	sess, err := p.DetectLatestSession(workspace)
	if err != nil {
		t.Fatalf("DetectLatestSession failed: %v", err)
	}
	details, err := p.ParseSession(sess.Path)
	if err != nil {
		t.Fatalf("ParseSession failed: %v", err)
	}
	if details.CooldownUntil == nil {
		t.Fatal("expected CooldownUntil from agy quota step")
	}
	anchor := time.Date(2026, 11, 13, 19, 36, 0, 0, time.UTC)
	want := anchor.Add(4*time.Hour + 8*time.Minute + 33*time.Second)
	if !details.CooldownUntil.Equal(want) {
		t.Errorf("expected reset at %v, got %v", want, details.CooldownUntil)
	}
}

func TestAgyAccountWideQuotaDetection(t *testing.T) {
	tempDir := t.TempDir()
	p := NewAgy(tempDir)
	now := time.Now()

	// Two conversations in different workspaces; the freshest carries the
	// active quota window.
	old := filepath.Join(tempDir, "brain", "conv-old", ".system_generated", "logs", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(old), 0755); err != nil {
		t.Fatal(err)
	}
	oldStep := fmt.Sprintf(`{"type":"ERROR_MESSAGE","error":"API error: RESOURCE_EXHAUSTED (code 429): Individual quota reached. Resets in 30m.","created_at":%q}`, now.Add(-2*time.Hour).Format(time.RFC3339))
	if err := os.WriteFile(old, []byte(oldStep+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	fresh := filepath.Join(tempDir, "brain", "conv-fresh", ".system_generated", "logs", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(fresh), 0755); err != nil {
		t.Fatal(err)
	}
	anchorStr := now.Add(-10 * time.Minute).UTC().Truncate(time.Second).Format(time.RFC3339)
	freshStep := fmt.Sprintf(`{"type":"ERROR_MESSAGE","error":"API error: RESOURCE_EXHAUSTED (code 429): Individual quota reached. Resets in 4h8m33s.","created_at":%q}`, anchorStr)
	if err := os.WriteFile(fresh, []byte(freshStep+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	until := p.DetectQuotaCooldown(now)
	if until == nil {
		t.Fatal("expected account-wide quota detection from freshest transcript")
	}
	anchor, _ := time.Parse(time.RFC3339, anchorStr)
	want := anchor.Add(4*time.Hour + 8*time.Minute + 33*time.Second)
	if !until.Equal(want) {
		t.Errorf("expected reset at ~%v, got %v", want, until)
	}
}

func TestAgyAccountWideQuotaIgnoresStaleTranscripts(t *testing.T) {
	tempDir := t.TempDir()
	p := NewAgy(tempDir)
	now := time.Now()

	// Quota window announced long ago in a transcript whose mtime is old:
	// skipped entirely (and the window is expired anyway).
	stale := filepath.Join(tempDir, "brain", "conv-stale", ".system_generated", "logs", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(stale), 0755); err != nil {
		t.Fatal(err)
	}
	step := `{"type":"ERROR_MESSAGE","error":"API error: RESOURCE_EXHAUSTED (code 429): Individual quota reached. Resets in 4h0m0s.","created_at":"2026-09-27T09:28:15Z"}` + "\n"
	if err := os.WriteFile(stale, []byte(step), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := now.AddDate(0, 0, -47)
	if err := os.Chtimes(stale, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	if until := p.DetectQuotaCooldown(now); until != nil {
		t.Errorf("expected nil for stale/expired quota window, got %v", until)
	}
}

func TestAgyAccountWideQuotaNoStore(t *testing.T) {
	p := NewAgy(t.TempDir() + "/nonexistent")
	if until := p.DetectQuotaCooldown(time.Now()); until != nil {
		t.Errorf("expected nil with no agy store, got %v", until)
	}
}
