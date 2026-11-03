package provider

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestOpenCodeBasics(t *testing.T) {
	p := NewOpenCode("/tmp/test-opencode")
	if p.Name() != "opencode" {
		t.Errorf("expected opencode, got %s", p.Name())
	}
	if p.BinaryName() != "opencode" {
		t.Errorf("expected opencode, got %s", p.BinaryName())
	}
	if p.DefaultCooldown() != 4*time.Hour {
		t.Errorf("expected 4h cooldown, got %v", p.DefaultCooldown())
	}

	emptyArgs := p.FormatExecArgs("")
	if !reflect.DeepEqual(emptyArgs, []string{"opencode"}) {
		t.Errorf("expected ['opencode'], got %v", emptyArgs)
	}

	promptArgs := p.FormatExecArgs("fix race condition")
	expected := []string{"opencode", "run", "-i", "fix race condition"}
	if !reflect.DeepEqual(promptArgs, expected) {
		t.Errorf("expected %v, got %v", expected, promptArgs)
	}
}

func TestOpenCodeModelIdentifierParsing(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"id":"xiaomi/mimo-v2.6-pro","providerID":"openrouter"}`, "mimo-v2.6-pro"},
		{`{"id":"deepseek-v4-flash-free","providerID":"opencode"}`, "deepseek-v4-flash-free"},
		{`moonshotai/kimi-k2.6`, "kimi-k2.6"},
		{`qwen/qwen3-coder-next`, "qwen3-coder-next"},
		{`gpt-5.5`, "gpt-5.5"},
		{`gemma4:e4b`, "gemma4:e4b"},
		{``, ""},
	}

	for _, tc := range tests {
		actual := parseModelIdentifier(tc.input)
		if actual != tc.expected {
			t.Errorf("parseModelIdentifier(%q) = %q, expected %q", tc.input, actual, tc.expected)
		}
	}
}

func TestOpenCodeSQLiteIntegration(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available in test environment")
	}

	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "opencode.db")

	initSQL := `
CREATE TABLE session (
	id text PRIMARY KEY,
	directory text NOT NULL,
	time_created integer NOT NULL,
	time_updated integer NOT NULL,
	model text,
	cost real DEFAULT 0 NOT NULL,
	tokens_input integer DEFAULT 0 NOT NULL,
	tokens_output integer DEFAULT 0 NOT NULL,
	tokens_reasoning integer DEFAULT 0 NOT NULL
);
CREATE TABLE message (
	id text PRIMARY KEY,
	session_id text NOT NULL,
	time_created integer NOT NULL,
	time_updated integer NOT NULL,
	data text NOT NULL
);
CREATE TABLE part (
	id text PRIMARY KEY,
	message_id text NOT NULL,
	session_id text NOT NULL,
	time_created integer NOT NULL,
	time_updated integer NOT NULL,
	data text NOT NULL
);

-- Insert session 1: target project
INSERT INTO session VALUES (
	'ses_001',
	'/Users/test/workspace',
	1700000000000,
	1700000500000,
	'{"id":"xiaomi/mimo-v2.6-pro","providerID":"openrouter"}',
	0.05,
	1000, 200, 50
);

-- Insert session 2: different project
INSERT INTO session VALUES (
	'ses_002',
	'/Users/test/other',
	1700000100000,
	1700000200000,
	'{"id":"qwen/qwen3-coder-next"}',
	0.02,
	500, 100, 0
);
`
	cmd := exec.Command("sqlite3", dbPath, initSQL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to init sqlite test db: %v (%s)", err, string(out))
	}

	now := time.Now()
	nowMs := now.UnixMilli()

	// Insert messages and parts for session 1
	populateSQL := fmt.Sprintf(`
-- User message
INSERT INTO message VALUES (
	'msg_user_1', 'ses_001', %d, %d,
	'{"role":"user","tokens":{"total":100,"input":100,"output":0,"reasoning":0}}'
);
INSERT INTO part VALUES (
	'prt_user_1', 'msg_user_1', 'ses_001', %d, %d,
	'{"type":"text","text":"Implement rate limiting"}'
);

-- Assistant message
INSERT INTO message VALUES (
	'msg_asst_1', 'ses_001', %d, %d,
	'{"role":"assistant","modelID":"xiaomi/mimo-v2.6-pro","cost":0.05,"tokens":{"total":1150,"input":900,"output":200,"reasoning":50},"time":{"created":%d,"completed":%d}}'
);
INSERT INTO part VALUES (
	'prt_asst_text', 'msg_asst_1', 'ses_001', %d, %d,
	'{"type":"text","text":"I will add the token bucket limiter."}'
);
INSERT INTO part VALUES (
	'prt_asst_tool', 'msg_asst_1', 'ses_001', %d, %d,
	'{"type":"tool","tool":"bash","state":{"input":{"command":"go test ./..."},"title":"Running test suite"}}'
);
`, nowMs-5000, nowMs-5000, nowMs-5000, nowMs-5000,
		nowMs, nowMs+2000, nowMs, nowMs+2000,
		nowMs, nowMs,
		nowMs+500, nowMs+500,
	)

	cmd = exec.Command("sqlite3", dbPath, populateSQL)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to populate sqlite test db: %v (%s)", err, string(out))
	}

	p := NewOpenCode(tempDir)

	// 1. DetectLatestSession
	sess, err := p.DetectLatestSession("/Users/test/workspace")
	if err != nil {
		t.Fatalf("DetectLatestSession failed: %v", err)
	}
	if sess.ID != "ses_001" {
		t.Errorf("expected session ses_001, got %s", sess.ID)
	}
	if sess.Provider != "opencode" {
		t.Errorf("expected provider opencode, got %s", sess.Provider)
	}

	// Nonexistent project
	_, err = p.DetectLatestSession("/Users/test/missing")
	if err == nil {
		t.Fatal("expected error for nonexistent cwd, got nil")
	}

	// 2. ParseSession
	details, err := p.ParseSession(sess.Path)
	if err != nil {
		t.Fatalf("ParseSession failed: %v", err)
	}
	if details.ID != "ses_001" {
		t.Errorf("expected ID ses_001, got %s", details.ID)
	}
	if details.LastUserMessage != "Implement rate limiting" {
		t.Errorf("expected last user message 'Implement rate limiting', got %q", details.LastUserMessage)
	}
	if details.LastAssistantMessage != "I will add the token bucket limiter." {
		t.Errorf("expected last assistant message, got %q", details.LastAssistantMessage)
	}
	if len(details.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(details.Turns))
	}
	if details.Turns[1].Tools[0] != `bash("Running test suite")` {
		t.Errorf("expected bash tool with title, got %s", details.Turns[1].Tools[0])
	}
	if len(details.ModelsUsed) != 1 || details.ModelsUsed[0] != "mimo-v2.6-pro" {
		t.Errorf("expected ['mimo-v2.6-pro'], got %v", details.ModelsUsed)
	}

	// 3. CollectUsage (Project Scoped)
	usageProj, err := p.CollectUsage(now, "/Users/test/workspace")
	if err != nil {
		t.Fatalf("CollectUsage failed: %v", err)
	}
	if usageProj.TotalSessions != 1 {
		t.Errorf("expected 1 session, got %d", usageProj.TotalSessions)
	}
	if usageProj.TotalTokens != 1250 {
		t.Errorf("expected 1250 total tokens (1000+200+50), got %d", usageProj.TotalTokens)
	}
	if usageProj.TokensToday != 1150 {
		t.Errorf("expected 1150 tokens today, got %d", usageProj.TokensToday)
	}
	if usageProj.LastModel != "mimo-v2.6-pro" {
		t.Errorf("expected last model mimo-v2.6-pro, got %s", usageProj.LastModel)
	}
	if len(usageProj.ModelStats) != 1 {
		t.Errorf("expected 1 model stat, got %d", len(usageProj.ModelStats))
	}
	mStat := usageProj.ModelStats["mimo-v2.6-pro"]
	if mStat.Turns != 1 {
		t.Errorf("expected 1 turn for model, got %d", mStat.Turns)
	}
	if mStat.TokensPerSec <= 0 {
		t.Errorf("expected positive tokens per sec, got %f", mStat.TokensPerSec)
	}
	if usageProj.TotalCostUSD != 0.05 {
		t.Errorf("expected 0.05 cost for project, got %f", usageProj.TotalCostUSD)
	}
	if mStat.CostUSD != 0.05 {
		t.Errorf("expected 0.05 cost for model, got %f", mStat.CostUSD)
	}

	// 4. CollectUsage (Global)
	usageGlobal, err := p.CollectUsage(now)
	if err != nil {
		t.Fatalf("CollectUsage global failed: %v", err)
	}
	if usageGlobal.TotalSessions != 2 {
		t.Errorf("expected 2 sessions global, got %d", usageGlobal.TotalSessions)
	}
	if usageGlobal.TotalTokens != 1850 { // 1250 + 600
		t.Errorf("expected 1850 total tokens global, got %d", usageGlobal.TotalTokens)
	}
	if usageGlobal.TotalCostUSD != 0.07 { // 0.05 + 0.02
		t.Errorf("expected 0.07 total cost global, got %f", usageGlobal.TotalCostUSD)
	}
}

func TestOpenCodeMissingDB(t *testing.T) {
	tempDir := t.TempDir()
	p := NewOpenCode(filepath.Join(tempDir, "nonexistent"))

	usage, err := p.CollectUsage(time.Now())
	if err != nil {
		t.Fatalf("expected nil error on missing db, got %v", err)
	}
	if usage.TotalSessions != 0 || usage.TotalTokens != 0 {
		t.Errorf("expected 0 sessions and 0 tokens on missing db, got %+v", usage)
	}

	_, err = p.DetectLatestSession("/Users/test")
	if err == nil {
		t.Fatal("expected error on DetectLatestSession for missing db, got nil")
	}

	ctx := context.Background()
	var rows []any
	err = p.queryJSON(ctx, "SELECT 1;", &rows)
	if err == nil {
		t.Fatal("expected error for query on missing db, got nil")
	}
}
