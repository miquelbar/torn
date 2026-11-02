package handoff

import (
	"fmt"
	"strings"
	"time"

	"torn/internal/project"
	"torn/internal/provider"
)

// Options holds flags controlling handoff synthesis.
type Options struct {
	DropLastNTurns int  // --drop N
	Fresh          bool // --fresh
	DryRun         bool // --dry-run
}

// Builder constructs high-density handoff prompts for AI agents.
type Builder struct {
	git project.GitRunner
}

// NewBuilder creates a new Builder with the default GitRunner.
func NewBuilder() *Builder {
	return &Builder{git: project.DefaultGitRunner}
}

// NewBuilderWithGit allows injecting a custom GitRunner for deterministic testing.
func NewBuilderWithGit(git project.GitRunner) *Builder {
	return &Builder{git: git}
}

// Build synthesizes a structured handoff prompt to pass to the target CLI.
func (b *Builder) Build(proj *project.Info, fromCLI string, toCLI string, details *provider.SessionDetails, opts Options) string {
	var sb strings.Builder

	sb.WriteString("# TORN RELAY HANDOFF\n\n")
	sb.WriteString(fmt.Sprintf("**Timestamp:** %s\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("**Workspace:** `%s`\n", proj.Path))
	if proj.IsGit && proj.GitHead != "" {
		sb.WriteString(fmt.Sprintf("**Git HEAD:** `%s`\n", proj.GitHead))
	}
	sb.WriteString(fmt.Sprintf("**Relay:** `%s` ➔ `%s`\n\n", fromCLI, toCLI))

	if opts.Fresh {
		sb.WriteString("> [!NOTE] Fresh Relay Requested. Prior handoff chain was cleared by user.\n\n")
		sb.WriteString("## Context\n")
		sb.WriteString("A fresh task is starting. Review repository structure, mission files (if present), and git status before proceeding.\n\n")
		sb.WriteString(b.renderGitStatus(proj))
		return sb.String()
	}

	sb.WriteString("## Active Task & Handover Context\n\n")

	// 1. User Intent & Last Directives
	sb.WriteString(b.renderUserIntent(details))

	// 2. Recent Assistant Reasoning & Actions
	sb.WriteString(b.renderAssistantActions(details, opts))

	// 3. Workspace Git Status
	sb.WriteString(b.renderGitStatus(proj))

	sb.WriteString("## Next Steps Instructions\n")
	sb.WriteString("1. Resume implementation from the state left by the previous agent.\n")
	sb.WriteString("2. Check tests and verify no regressions exist.\n")
	sb.WriteString("3. Adhere to repository coding and architectural constraints.\n")

	return sb.String()
}

func (b *Builder) renderUserIntent(details *provider.SessionDetails) string {
	var sb strings.Builder
	sb.WriteString("### User Intent & Last Directives\n\n")

	if details == nil || (len(details.Turns) == 0 && details.LastUserMessage == "") {
		sb.WriteString("*(No prior user directives recorded in session)*\n\n")
		return sb.String()
	}

	// Filter human user turns across entire session (ignoring tool results)
	var userTurns []string
	for _, t := range details.Turns {
		if t.Role == "user" {
			txt := strings.TrimSpace(t.Text)
			if txt != "" && !isToolResultText(txt) {
				userTurns = append(userTurns, txt)
			}
		}
	}

	if len(userTurns) == 0 && details.LastUserMessage != "" && !isToolResultText(details.LastUserMessage) {
		userTurns = append(userTurns, strings.TrimSpace(details.LastUserMessage))
	}

	if len(userTurns) == 0 {
		sb.WriteString("*(No explicit human user directives identified)*\n\n")
		return sb.String()
	}

	// Root Objective: first user message
	root := truncateText(userTurns[0], 500)
	sb.WriteString(fmt.Sprintf("**Root Objective:**\n> %s\n\n", root))

	// Last directives (up to last 3 user messages)
	if len(userTurns) > 1 {
		sb.WriteString("**Recent Directives:**\n")
		startIdx := len(userTurns) - 3
		if startIdx < 1 {
			startIdx = 1
		}
		for i := startIdx; i < len(userTurns); i++ {
			msg := truncateText(userTurns[i], 300)
			sb.WriteString(fmt.Sprintf("- %s\n", msg))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (b *Builder) renderAssistantActions(details *provider.SessionDetails, opts Options) string {
	var sb strings.Builder
	sb.WriteString("### Recent Assistant Reasoning & Actions\n\n")

	if details == nil || (len(details.Turns) == 0 && details.LastAssistantMessage == "") {
		sb.WriteString("*(No assistant actions recorded)*\n\n")
		return sb.String()
	}

	turns := details.Turns
	if opts.DropLastNTurns > 0 && len(turns) > 0 {
		cutoff := len(turns) - opts.DropLastNTurns
		if cutoff < 0 {
			cutoff = 0
		}
		turns = turns[:cutoff]
		sb.WriteString(fmt.Sprintf("*Notice: Last %d turns were pruned via `--drop` to eliminate stuck loops.*\n\n", opts.DropLastNTurns))
	}

	var assistantTurns []provider.Turn
	for _, t := range turns {
		if t.Role == "assistant" && (strings.TrimSpace(t.Text) != "" || len(t.Tools) > 0) {
			assistantTurns = append(assistantTurns, t)
		}
	}

	if len(assistantTurns) > 0 {
		startIdx := 0
		if len(assistantTurns) > 5 {
			startIdx = len(assistantTurns) - 5
		}
		for i := startIdx; i < len(assistantTurns); i++ {
			at := assistantTurns[i]
			txt := strings.TrimSpace(at.Text)
			if txt != "" {
				sb.WriteString(fmt.Sprintf("- **Reasoning:** %s\n", truncateText(txt, 400)))
			}
			if len(at.Tools) > 0 {
				sb.WriteString(fmt.Sprintf("  *Actions:* `%s`\n", strings.Join(at.Tools, "`, `")))
			}
		}
		sb.WriteString("\n")
	} else if details.LastAssistantMessage != "" {
		sb.WriteString(fmt.Sprintf("- **Reasoning:** %s\n\n", truncateText(details.LastAssistantMessage, 400)))
	} else {
		sb.WriteString("*(No outgoing assistant actions)*\n\n")
	}

	return sb.String()
}

func (b *Builder) renderGitStatus(proj *project.Info) string {
	var sb strings.Builder
	sb.WriteString("### Workspace Git Status\n\n")

	if !proj.IsGit || b.git == nil {
		sb.WriteString("Workspace is not a Git repository.\n\n")
		return sb.String()
	}

	statusOut, _ := b.git(proj.Path, "status", "-s")
	diffOut, _ := b.git(proj.Path, "diff", "--stat")

	statusOut = strings.TrimSpace(statusOut)
	diffOut = strings.TrimSpace(diffOut)

	if statusOut == "" && diffOut == "" {
		sb.WriteString("Working tree clean (no uncommitted changes).\n\n")
		return sb.String()
	}

	if statusOut != "" {
		sb.WriteString("**Modified files (`git status -s`):**\n```txt\n")
		sb.WriteString(statusOut)
		sb.WriteString("\n```\n\n")
	}

	if diffOut != "" {
		sb.WriteString("**Diff summary (`git diff --stat`):**\n```txt\n")
		sb.WriteString(diffOut)
		sb.WriteString("\n```\n\n")
	}

	return sb.String()
}

func isToolResultText(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return true
	}
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "tool_result") || strings.HasPrefix(lower, "tool result") {
		return true
	}
	if strings.HasPrefix(trimmed, `{"tool_call_id"`) || strings.HasPrefix(trimmed, `{"output":`) || strings.HasPrefix(trimmed, `{"is_error":`) {
		return true
	}
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		if strings.Contains(trimmed, `"tool_use_id"`) || strings.Contains(trimmed, `"tool_call_id"`) || strings.Contains(trimmed, `"output"`) {
			return true
		}
	}
	return false
}

func truncateText(text string, maxLen int) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) <= maxLen {
		return trimmed
	}
	return trimmed[:maxLen] + "... [truncated]"
}
