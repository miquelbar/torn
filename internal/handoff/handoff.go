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
type Builder struct{}

// NewBuilder creates a new Builder.
func NewBuilder() *Builder {
	return &Builder{}
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
		sb.WriteString("A fresh task is starting. Review repository structure, mission files (if present), and git status before proceeding.\n")
		return sb.String()
	}

	sb.WriteString("## Active Task & Handover Context\n\n")

	turns := details.Turns
	if opts.DropLastNTurns > 0 && len(turns) > 0 {
		cutoff := len(turns) - opts.DropLastNTurns
		if cutoff < 0 {
			cutoff = 0
		}
		turns = turns[:cutoff]
		sb.WriteString(fmt.Sprintf("*Notice: Last %d turns were pruned via `--drop` to eliminate stuck loops.*\n\n", opts.DropLastNTurns))
	}

	if len(turns) > 0 {
		sb.WriteString("### Recent Progress Summary\n\n")
		startIdx := 0
		if len(turns) > 5 {
			startIdx = len(turns) - 5
		}
		for i := startIdx; i < len(turns); i++ {
			t := turns[i]
			roleLabel := "User"
			if t.Role == "assistant" {
				roleLabel = "Assistant"
			}
			text := strings.TrimSpace(t.Text)
			if len(text) > 400 {
				text = text[:400] + "... [truncated]"
			}
			if text != "" {
				sb.WriteString(fmt.Sprintf("- **%s**: %s\n", roleLabel, text))
			}
			if len(t.Tools) > 0 {
				sb.WriteString(fmt.Sprintf("  *Actions:* `%s`\n", strings.Join(t.Tools, "`, `")))
			}
		}
		sb.WriteString("\n")
	} else if details.LastAssistantMessage != "" {
		sb.WriteString("### Last Known Status\n\n")
		text := strings.TrimSpace(details.LastAssistantMessage)
		if len(text) > 600 {
			text = text[:600] + "... [truncated]"
		}
		sb.WriteString(fmt.Sprintf("%s\n\n", text))
	}

	sb.WriteString("## Next Steps Instructions\n")
	sb.WriteString("1. Resume implementation from the state left by the previous agent.\n")
	sb.WriteString("2. Check tests and verify no regressions exist.\n")
	sb.WriteString("3. Adhere to repository coding and architectural constraints.\n")

	return sb.String()
}
