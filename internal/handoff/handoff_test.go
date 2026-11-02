package handoff

import (
	"strings"
	"testing"

	"torn/internal/project"
	"torn/internal/provider"
)

func TestBuilderStandardHandoff(t *testing.T) {
	b := NewBuilder()
	proj := &project.Info{
		Path:    "/Users/dev/app",
		GitHead: "abcdef123456",
		IsGit:   true,
	}

	details := &provider.SessionDetails{
		Turns: []provider.Turn{
			{Role: "user", Text: "implement endpoint /api/users"},
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
	if !strings.Contains(prompt, "endpoint created, need to add tests") {
		t.Errorf("prompt missing progress turn: %s", prompt)
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
	if !strings.Contains(prompt, "Pruned via `--drop`") && !strings.Contains(prompt, "`--drop`") {
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
