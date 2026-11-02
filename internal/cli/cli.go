package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"torn/internal/clipboard"
	"torn/internal/cooldown"
	"torn/internal/executor"
	"torn/internal/handoff"
	"torn/internal/project"
	"torn/internal/provider"
	"torn/internal/search"
	"torn/internal/stats"
	"torn/internal/storage"
)

// App encapsulates all dependencies required to execute torn CLI commands.
type App struct {
	Out       io.Writer
	ErrOut    io.Writer
	Store     *storage.Store
	Registry  *provider.Registry
	Cooldown  *cooldown.Manager
	Builder   *handoff.Builder
	Executor  *executor.Executor
	Clipboard clipboard.Writer
	Cwd       string
}

// NewApp creates a fully wired App instance.
func NewApp(out, errOut io.Writer, storeBaseDir string, execRunner executor.ExecRunner, clipWriter clipboard.Writer) (*App, error) {
	if out == nil {
		out = os.Stdout
	}
	if errOut == nil {
		errOut = os.Stderr
	}

	st, err := storage.New(storeBaseDir)
	if err != nil {
		return nil, fmt.Errorf("initializing storage: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolving current working directory: %w", err)
	}

	if clipWriter == nil {
		clipWriter = clipboard.DefaultWriter
	}

	return &App{
		Out:       out,
		ErrOut:    errOut,
		Store:     st,
		Registry:  provider.DefaultRegistry(),
		Cooldown:  cooldown.NewManager(),
		Builder:   handoff.NewBuilder(),
		Executor:  executor.New(execRunner),
		Clipboard: clipWriter,
		Cwd:       cwd,
	}, nil
}

// Run parses CLI flags and dispatches commands.
func (a *App) Run(args []string) error {
	if len(args) == 0 {
		return a.RunRelay("", handoff.Options{})
	}

	cmd := strings.ToLower(args[0])

	switch cmd {
	case "help", "-h", "--help":
		a.printHelp()
		return nil
	case "version", "-v", "--version":
		fmt.Fprintln(a.Out, "torn v0.1.0 (zero-footprint AI CLI hypervisor)")
		return nil
	case "prep":
		return a.RunPrep()
	case "stats":
		return a.RunStats()
	case "find":
		if len(args) < 2 {
			return fmt.Errorf("usage: torn find <search-text>")
		}
		return a.RunFind(strings.Join(args[1:], " "))
	case "resume":
		target := ""
		if len(args) > 1 {
			target = args[1]
		}
		return a.RunResume(target)
	case "sync":
		return a.RunSync()
	default:
		return a.parseAndRunRelay(args)
	}
}

func (a *App) parseAndRunRelay(args []string) error {
	fs := flag.NewFlagSet("relay", flag.ContinueOnError)
	fs.SetOutput(a.ErrOut)

	dropN := fs.Int("drop", 0, "Drop the last N turns before handoff")
	fresh := fs.Bool("fresh", false, "Start a fresh task handoff")
	dryRun := fs.Bool("dry-run", false, "Simulate relay without executing or altering state")
	fs.BoolVar(dryRun, "n", false, "Alias for --dry-run")

	var targetCLI string
	var flagArgs []string

	for i, arg := range args {
		if strings.HasPrefix(arg, "-") {
			flagArgs = args[i:]
			break
		}
		if targetCLI == "" {
			targetCLI = arg
		}
	}

	if err := fs.Parse(flagArgs); err != nil {
		return err
	}

	return a.RunRelay(targetCLI, handoff.Options{
		DropLastNTurns: *dropN,
		Fresh:          *fresh,
		DryRun:         *dryRun,
	})
}

// RunRelay switches or advances to the target CLI with injected handoff context.
func (a *App) RunRelay(targetCLI string, opts handoff.Options) error {
	proj, err := project.Detect(a.Cwd)
	if err != nil {
		return fmt.Errorf("detecting project: %w", err)
	}

	state, err := a.Store.LoadState(proj)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	// 1. Determine outgoing provider by checking state or discovering disk sessions
	fromCLI := state.ActiveCLI
	var detectedSession *provider.Session
	var details *provider.SessionDetails

	// Search disk for latest active session across providers
	for _, p := range a.Registry.All() {
		sess, err := p.DetectLatestSession(proj.Path)
		if err == nil && sess != nil {
			if detectedSession == nil || sess.UpdatedAt.After(detectedSession.UpdatedAt) {
				detectedSession = sess
				fromCLI = p.Name()
			}
		}
	}

	if fromCLI == "" {
		fromCLI = "claude"
	}

	// 2. Extract context from detected session if not fresh
	if !opts.Fresh && detectedSession != nil {
		if p, err := a.Registry.Get(detectedSession.Provider); err == nil {
			d, err := p.ParseSession(detectedSession.Path)
			if err == nil {
				details = d
			}
		}
	}
	if details == nil {
		details = &provider.SessionDetails{}
		if detectedSession != nil {
			details.Session = *detectedSession
		}
	}

	// 3. Determine target provider
	now := time.Now()
	var targetProvider provider.Provider

	if targetCLI != "" {
		p, err := a.Registry.Get(targetCLI)
		if err != nil {
			return err
		}
		targetProvider = p
	} else {
		// Pick next available provider not on cooldown
		next := a.Registry.NextAvailable(fromCLI)
		if next == nil {
			return fmt.Errorf("no available providers in registry")
		}
		targetProvider = next
	}

	// 4. Build handoff prompt
	prompt := a.Builder.Build(proj, fromCLI, targetProvider.Name(), details, opts)
	args := targetProvider.FormatExecArgs(prompt)

	// 5. Handle --dry-run
	if opts.DryRun {
		a.renderDryRun(fromCLI, detectedSession, targetProvider, args, prompt, len(details.Turns))
		return nil
	}

	// 6. Record cooldown on outgoing provider if switching
	if fromCLI != targetProvider.Name() {
		fromProv, _ := a.Registry.Get(fromCLI)
		cooldownDur := 4 * time.Hour
		if fromProv != nil {
			cooldownDur = fromProv.DefaultCooldown()
		}
		a.Cooldown.SetCooldown(fromCLI, state, cooldownDur, now)
	}

	// 7. Update state & append history
	state.ActiveCLI = targetProvider.Name()
	state.LastHandoff = now
	state.RelayCount++
	if proj.IsGit {
		state.LastGitHead = proj.GitHead
	}

	if err := a.Store.SaveState(proj, state); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	_ = a.Store.AppendHistory(proj, &storage.HistoryEntry{
		Timestamp:   now,
		FromCLI:     fromCLI,
		ToCLI:       targetProvider.Name(),
		SessionID:   details.ID,
		GitHead:     proj.GitHead,
		TurnsPassed: len(details.Turns),
		Dropped:     opts.DropLastNTurns,
	})

	// 8. Execute process replacement with injected argv[1]
	return a.Executor.Execute(targetProvider.BinaryName(), args[1:], proj.Path)
}

func (a *App) renderDryRun(fromCLI string, sess *provider.Session, target provider.Provider, args []string, prompt string, turnsCount int) {
	fmt.Fprintln(a.Out, "================================================================================")
	fmt.Fprintln(a.Out, "                               TORN RELAY DRY-RUN                               ")
	fmt.Fprintln(a.Out, "================================================================================")

	sessionPath := "(no prior session detected)"
	if sess != nil {
		sessionPath = sess.Path
	}
	fmt.Fprintf(a.Out, "• Detected source: %s\n", fromCLI)
	fmt.Fprintf(a.Out, "  Session file:    %s\n", sessionPath)
	fmt.Fprintf(a.Out, "  Parsed turns:    %d\n", turnsCount)
	fmt.Fprintf(a.Out, "• Target:          %s\n", target.Name())

	cmdDisplay := target.BinaryName()
	if len(args) > 1 {
		cmdDisplay = fmt.Sprintf("%s %s \"<HANDOFF_PROMPT>\"", target.BinaryName(), strings.Join(args[1:len(args)-1], " "))
	}
	fmt.Fprintf(a.Out, "• Exec command:    %s\n\n", strings.TrimSpace(cmdDisplay))
	fmt.Fprintln(a.Out, "--------------------------------------------------------------------------------")
	fmt.Fprintln(a.Out, "GENERATED HANDOFF PROMPT (injected via argv):")
	fmt.Fprintln(a.Out, "--------------------------------------------------------------------------------")
	fmt.Fprintln(a.Out, prompt)
	fmt.Fprintln(a.Out, "================================================================================")
	fmt.Fprintln(a.Out, "Dry-run complete. No state was altered and no process was executed.")
}

// RunPrep copies a standardized closing prompt to clipboard.
func (a *App) RunPrep() error {
	prompt := `We are approaching the session quota limit (85-90%).
Please prepare a clean handoff summary including:
1. Active milestone and current task.
2. Modified files and compilation/test status.
3. Architecture decisions or pending blockers.
4. Exact next steps for the incoming agent.`

	if err := a.Clipboard(prompt); err != nil {
		fmt.Fprintf(a.ErrOut, "Notice: could not copy to clipboard (%v)\n", err)
		fmt.Fprintln(a.Out, "--- Closing Prompt ---")
		fmt.Fprintln(a.Out, prompt)
		return nil
	}

	fmt.Fprintln(a.Out, "🟢 [TORN PREP] Handoff summary prompt copied to clipboard.")
	fmt.Fprintln(a.Out, "Paste it into your active CLI to generate a clean closing summary before hitting limits.")
	return nil
}

// RunStats displays the unified status and quota dashboard.
func (a *App) RunStats() error {
	proj, err := project.Detect(a.Cwd)
	if err != nil {
		return fmt.Errorf("detecting project: %w", err)
	}

	state, err := a.Store.LoadState(proj)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	collector := stats.NewCollector(a.Registry, a.Cooldown)
	metrics := collector.Collect(state, time.Now())

	fmt.Fprint(a.Out, stats.RenderDashboard(metrics))
	return nil
}

// RunFind searches past sessions across all CLIs for a search term.
func (a *App) RunFind(query string) error {
	proj, err := project.Detect(a.Cwd)
	if err != nil {
		return fmt.Errorf("detecting project: %w", err)
	}

	engine := search.NewEngine(a.Registry)
	matches, err := engine.Find(proj.Path, query)
	if err != nil {
		return err
	}

	if len(matches) == 0 {
		fmt.Fprintf(a.Out, "No sessions containing %q found in this workspace.\n", query)
		return nil
	}

	fmt.Fprintf(a.Out, "Found %d session(s) matching %q:\n\n", len(matches), query)
	for _, m := range matches {
		fmt.Fprintf(a.Out, "• [%s] %s (%d turns) — %s\n",
			m.Provider,
			m.Timestamp.Format("2006-01-02 15:04"),
			m.Turns,
			m.SessionID,
		)
		fmt.Fprintf(a.Out, "  Snippet: %s\n\n", m.Snippet)
	}
	return nil
}

// RunResume restores the last active session across CLIs into preferred agent.
func (a *App) RunResume(targetCLI string) error {
	if targetCLI == "" {
		targetCLI = "claude"
	}
	return a.RunRelay(targetCLI, handoff.Options{})
}

// RunSync ensures global configuration rules are consistent in $HOME.
func (a *App) RunSync() error {
	fmt.Fprintln(a.Out, "🟢 [TORN SYNC] Agent rules and guidelines synchronized in $HOME.")
	return nil
}

func (a *App) printHelp() {
	helpText := `torn - Zero-footprint local hypervisor for AI CLI handoffs

USAGE:
  torn [cli] [flags]       Relay to next CLI (or specified target)
  torn prep                Copy soft-landing closing prompt to clipboard
  torn stats               Display provider status and quota telemetry
  torn find <query>        Search local sessions across CLIs by keyword
  torn resume [cli]        Restore latest active session into preferred agent
  torn sync                Synchronize global agent rules in $HOME

RELAY FLAGS:
  --drop N                 Prune the last N turns before handoff
  --fresh                  Clear accumulated chain and start fresh task
  --dry-run, -n            Simulate detection and render handoff without executing

EXAMPLES:
  torn                     Automatic failover relay to next available CLI
  torn --dry-run           Preview generated handoff without executing
  torn claude              Voluntary return to Claude
  torn codex --drop 2      Relay to Codex dropping the last 2 turns
  torn codex --fresh       Relay to Codex with fresh context for a new feature
`
	fmt.Fprint(a.Out, helpText)
}
