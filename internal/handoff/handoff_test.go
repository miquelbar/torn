package handoff

import (
	"strings"
	"testing"

	"torn/internal/project"
	"torn/internal/provider"
)

func TestBuilderStandardHandoff(t *testing.T) {
	mockGit := func(dir string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "status" {
			return " M file.go", nil
		}
		if len(args) > 0 && args[0] == "diff" {
			return " file.go | 2 +-\n 1 file changed", nil
		}
		return "", nil
	}

	b := NewBuilderWithGit(mockGit)
	proj := &project.Info{
		Path:    "/Users/dev/app",
		GitHead: "abcdef123456",
		IsGit:   true,
	}

	details := &provider.SessionDetails{
		Turns: []provider.Turn{
			{Role: "user", Text: "implement endpoint /api/users"},
			{Role: "assistant", Text: "thinking about model", Tools: []string{"write_file(user.go)"}},
			{Role: "user", Text: "tool_result: success"}, // should be filtered out
			{Role: "user", Text: "make sure tests pass"},
			{Role: "assistant", Text: "endpoint created, need to add tests"},
		},
	}

	prompt := b.Build(proj, "claude", "codex", details, Options{})

	if !strings.Contains(prompt, "`claude` ➔ `codex`") {
		t.Errorf("prompt missing relay path: %s", prompt)
	}
	if !strings.Contains(prompt, "abcdef123456") {
		t.Errorf("prompt missing git head: %s", prompt)
	}

	// 1. Verify User Intent section
	if !strings.Contains(prompt, "### User Intent & Last Directives") {
		t.Errorf("prompt missing User Intent section: %s", prompt)
	}
	if !strings.Contains(prompt, "**Root Objective:**") || !strings.Contains(prompt, "implement endpoint /api/users") {
		t.Errorf("prompt missing root objective: %s", prompt)
	}
	if !strings.Contains(prompt, "make sure tests pass") {
		t.Errorf("prompt missing recent directive: %s", prompt)
	}
	if strings.Contains(prompt, "tool_result: success") {
		t.Errorf("prompt should have filtered out tool_result from user directives: %s", prompt)
	}

	// 2. Verify Assistant Reasoning & Actions section
	if !strings.Contains(prompt, "### Recent Assistant Reasoning & Actions") {
		t.Errorf("prompt missing Assistant Actions section: %s", prompt)
	}
	if !strings.Contains(prompt, "endpoint created, need to add tests") {
		t.Errorf("prompt missing assistant reasoning: %s", prompt)
	}
	if !strings.Contains(prompt, "write_file(user.go)") {
		t.Errorf("prompt missing tool action: %s", prompt)
	}

	// 3. Verify Git Status section
	if !strings.Contains(prompt, "### Workspace Git Status") {
		t.Errorf("prompt missing Git Status section: %s", prompt)
	}
	if !strings.Contains(prompt, "M file.go") {
		t.Errorf("prompt missing git status output: %s", prompt)
	}
	if !strings.Contains(prompt, "1 file changed") {
		t.Errorf("prompt missing git diff output: %s", prompt)
	}
}

func TestBuilderDropTurns(t *testing.T) {
	b := NewBuilder()
	proj := &project.Info{Path: "/app"}

	details := &provider.SessionDetails{
		Turns: []provider.Turn{
			{Role: "user", Text: "first prompt"},
			{Role: "assistant", Text: "hallucination loop 1"},
			{Role: "assistant", Text: "hallucination loop 2"},
		},
	}

	prompt := b.Build(proj, "claude", "codex", details, Options{DropLastNTurns: 2})

	if strings.Contains(prompt, "hallucination loop 2") {
		t.Errorf("prompt should have dropped hallucination loop 2")
	}
	if !strings.Contains(prompt, "first prompt") {
		t.Errorf("prompt should retain earlier turns")
	}
	if !strings.Contains(prompt, "pruned via `--drop`") && !strings.Contains(prompt, "`--drop`") {
		t.Errorf("prompt should mention drop notice")
	}
}

func TestBuilderFresh(t *testing.T) {
	b := NewBuilder()
	proj := &project.Info{Path: "/app"}

	details := &provider.SessionDetails{
		Turns: []provider.Turn{
			{Role: "user", Text: "old task"},
		},
	}

	prompt := b.Build(proj, "claude", "codex", details, Options{Fresh: true})

	if strings.Contains(prompt, "old task") {
		t.Errorf("fresh prompt should not contain old task turns")
	}
	if !strings.Contains(prompt, "Fresh Relay Requested") {
		t.Errorf("expected Fresh Relay notice")
	}
}

func TestBuilderUserDirectivesPreservationAndDeduplication(t *testing.T) {
	b := NewBuilder()
	proj := &project.Info{Path: "/app"}

	longDirective := "Please fix the following 3 issues:\n1. Translate everything to English.\n2. Fix truncation so long instructions are never cut off prematurely.\n3. Verify all historical tokens accurately without inflation."

	details := &provider.SessionDetails{
		Turns: []provider.Turn{
			{Role: "user", Text: "Root objective: configure repository"},
			{Role: "assistant", Text: "Done."},
			{Role: "user", Text: "Audit the system thoroughly."},
			{Role: "user", Text: "Audit the system thoroughly."}, // consecutive duplicate
			{Role: "assistant", Text: "Audit complete."},
			{Role: "user", Text: longDirective}, // latest user directive (must be fully preserved)
		},
	}

	prompt := b.Build(proj, "claude", "codex", details, Options{})

	// 1. Verify Root Objective
	if !strings.Contains(prompt, "Root objective: configure repository") {
		t.Errorf("expected root objective")
	}

	// 2. Verify duplicate removal (should only appear once)
	count := strings.Count(prompt, "Audit the system thoroughly.")
	if count != 1 {
		t.Errorf("expected duplicate directive to appear exactly once, got %d", count)
	}

	// 3. Verify latest directive is fully preserved with all 3 points
	if !strings.Contains(prompt, "3. Verify all historical tokens accurately without inflation.") {
		t.Errorf("expected latest directive to be preserved in full without truncation")
	}
}

func TestBuilderPseudoUserFilteringAndQuotaLimitFiltering(t *testing.T) {
	b := NewBuilder()
	proj := &project.Info{Path: "/app"}

	compactionBanner := "This session is being continued from a previous conversation that ran out of context. " + strings.Repeat("Detailed summary point. ", 40)
	lastHumanPrompt := "Please verify phase 1C before concluding: all tests passing and no regressions."
	taskNotification := "<task-notification>\n<task-id>bg85utuzc</task-id>\n<summary>Background command \"run tests\" completed (exit code 0)</summary>\n</task-notification>"
	substantialReasoning := "Waiting for the suite result. Meanwhile, here is the state of Brief 21: " + strings.Repeat("Verified module A. ", 30)
	quotaBanner := "You've hit your session limit · resets 12:30pm (Europe/Madrid) · progress saved"

	details := &provider.SessionDetails{
		Turns: []provider.Turn{
			{Role: "user", Text: compactionBanner},
			{Role: "assistant", Text: substantialReasoning},
			{Role: "user", Text: lastHumanPrompt},
			{Role: "assistant", Text: quotaBanner},
			{Role: "user", Text: taskNotification},
			{Role: "assistant", Text: quotaBanner},
		},
	}

	prompt := b.Build(proj, "claude", "codex", details, Options{})

	// 1. Task notification must NOT be under User Directives
	if strings.Contains(prompt, "<task-notification>") {
		t.Errorf("task-notification should be filtered from handoff prompt")
	}

	// 2. Task notification summary should appear as assistant action
	if !strings.Contains(prompt, "task_notification(Background command \"run tests\" completed (exit code 0))") {
		t.Errorf("expected task notification summary as action, prompt: %s", prompt)
	}

	// 3. The last human prompt must be preserved in full (not truncated)
	if !strings.Contains(prompt, lastHumanPrompt) {
		t.Errorf("expected last human prompt to be fully preserved: %s", prompt)
	}

	// 4. Compaction banner in Root Objective should retain extended length (up to 1500 chars)
	if !strings.Contains(prompt, "Detailed summary point.") {
		t.Errorf("expected compaction banner in root objective")
	}

	// 5. Quota limit banner must NOT appear in Reasoning
	if strings.Contains(prompt, "You've hit your session limit") {
		t.Errorf("quota limit banner should be filtered from reasoning: %s", prompt)
	}

	// 6. Substantial reasoning should be preserved with generous limit (>400 chars)
	if !strings.Contains(prompt, "Waiting for the suite result. Meanwhile, here is the state of Brief 21:") {
		t.Errorf("expected substantial assistant reasoning")
	}
	if !strings.Contains(prompt, "Verified module A.") {
		t.Errorf("expected substantial reasoning to have generous limit")
	}
}

func TestBuilderSourceModelsHeader(t *testing.T) {
	b := NewBuilder()
	proj := &project.Info{Path: "/app"}

	details := &provider.SessionDetails{
		ModelsUsed: []string{"claude-opus-4-6", "claude-sonnet-4-5"},
		Turns: []provider.Turn{
			{Role: "user", Text: "hello"},
			{Role: "assistant", Text: "world"},
		},
	}

	prompt := b.Build(proj, "claude", "codex", details, Options{})

	expectedHeader := "**Source Models:** claude-opus-4-6 ➔ claude-sonnet-4-5\n**Relay:** `claude` ➔ `codex`"
	if !strings.Contains(prompt, expectedHeader) {
		t.Errorf("expected header %q in prompt, got:\n%s", expectedHeader, prompt)
	}
}
