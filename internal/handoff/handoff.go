package handoff

import (
	"fmt"
	"regexp"
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

	// Filter human user turns across entire session (ignoring tool results, pseudo-user messages, and consecutive duplicates)
	var userTurns []string
	for _, t := range details.Turns {
		if t.Role == "user" {
			txt := strings.TrimSpace(t.Text)
			if txt != "" && !isIgnoredUserMessage(txt) {
				if len(userTurns) > 0 && userTurns[len(userTurns)-1] == txt {
					continue
				}
				userTurns = append(userTurns, txt)
			}
		}
	}

	if len(userTurns) == 0 && details.LastUserMessage != "" && !isIgnoredUserMessage(details.LastUserMessage) {
		lastMsg := strings.TrimSpace(details.LastUserMessage)
		if len(userTurns) == 0 || userTurns[len(userTurns)-1] != lastMsg {
			userTurns = append(userTurns, lastMsg)
		}
	}

	if len(userTurns) == 0 {
		sb.WriteString("*(No explicit human user directives identified)*\n\n")
		return sb.String()
	}

	// Root Objective: first user message (up to 1500 chars if continuation banner, else 600)
	rootLimit := 600
	if strings.HasPrefix(userTurns[0], "This session is being continued from a previous conversation") {
		rootLimit = 1500
	}
	root := truncateText(userTurns[0], rootLimit)
	sb.WriteString(fmt.Sprintf("**Root Objective:**\n> %s\n\n", root))

	// Last directives (up to last 3 user messages)
	if len(userTurns) > 1 {
		sb.WriteString("**Recent Directives:**\n")
		startIdx := len(userTurns) - 3
		if startIdx < 1 {
			startIdx = 1
		}
		for i := startIdx; i < len(userTurns); i++ {
			isLatest := (i == len(userTurns)-1)
			limit := 600
			if isLatest {
				limit = 4000
			}
			msg := truncateText(userTurns[i], limit)
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
		if t.Role == "user" && strings.Contains(t.Text, "<task-notification>") {
			if summary := extractTaskNotificationSummary(t.Text); summary != "" {
				assistantTurns = append(assistantTurns, provider.Turn{
					Role:  "assistant",
					Tools: []string{fmt.Sprintf("task_notification(%s)", summary)},
				})
			}
			continue
		}

		if t.Role == "assistant" {
			txt := strings.TrimSpace(t.Text)
			if isQuotaLimitMessage(txt) {
				txt = ""
			}
			if txt != "" || len(t.Tools) > 0 {
				if len(assistantTurns) > 0 {
					prev := assistantTurns[len(assistantTurns)-1]
					if prev.Text == txt && len(t.Tools) == 0 && len(prev.Tools) == 0 {
						continue
					}
				}
				assistantTurns = append(assistantTurns, provider.Turn{
					Role:   "assistant",
					Text:   txt,
					Tools:  t.Tools,
					Tokens: t.Tokens,
				})
			}
		}
	}

	if len(assistantTurns) > 0 {
		startIdx := 0
		if len(assistantTurns) > 5 {
			startIdx = len(assistantTurns) - 5
		}

		lastTextIdx := -1
		for i := len(assistantTurns) - 1; i >= startIdx; i-- {
			if strings.TrimSpace(assistantTurns[i].Text) != "" {
				lastTextIdx = i
				break
			}
		}

		for i := startIdx; i < len(assistantTurns); i++ {
			at := assistantTurns[i]
			txt := strings.TrimSpace(at.Text)
			if txt != "" {
				limit := 400
				if i == lastTextIdx {
					limit = 2500
				}
				sb.WriteString(fmt.Sprintf("- **Reasoning:** %s\n", truncateText(txt, limit)))
			}
			if len(at.Tools) > 0 {
				sb.WriteString(fmt.Sprintf("  *Actions:* `%s`\n", strings.Join(at.Tools, "`, `")))
			}
		}
		sb.WriteString("\n")
	} else if details.LastAssistantMessage != "" && !isQuotaLimitMessage(details.LastAssistantMessage) {
		sb.WriteString(fmt.Sprintf("- **Reasoning:** %s\n\n", truncateText(details.LastAssistantMessage, 2500)))
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

var taskSummaryRegex = regexp.MustCompile(`(?s)<summary>(.*?)</summary>`)

func extractTaskNotificationSummary(s string) string {
	m := taskSummaryRegex.FindStringSubmatch(s)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func isQuotaLimitMessage(s string) bool {
	lower := strings.ToLower(s)
	return strings.Contains(lower, "you've hit your session limit") ||
		strings.Contains(lower, "rate limit reached") ||
		strings.Contains(lower, "hit your daily limit") ||
		strings.Contains(lower, "usage limit reached") ||
		strings.Contains(lower, "session limit reached")
}

func isIgnoredUserMessage(s string) bool {
	if isToolResultText(s) {
		return true
	}
	trimmed := strings.TrimSpace(s)
	prefixes := []string{
		"<task-notification",
		"<command-message",
		"<command-name",
		"<local-command-stdout",
		"<bash-stdout",
		"<bash-stderr",
		"[Request interrupted",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(trimmed, p) {
			return true
		}
	}
	return false
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
