package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	"torn/internal/ui"
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
	// Selector resolves the target CLI interactively before a bare relay.
	// When nil, torn falls back to its default interactive selector (or to
	// automatic failover when the terminal is not interactive).
	Selector ProviderSelector
}

// ProviderSelector interactively resolves a target CLI before a bare relay.
// target is the chosen provider name. cancelled=true aborts the relay
// cleanly. target=="" with cancelled==false signals "no interactive
// selection" (the caller falls back to automatic failover).
type ProviderSelector func() (target string, cancelled bool, err error)

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
		return a.runBareRelay()
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
		fs := flag.NewFlagSet("stats", flag.ContinueOnError)
		fs.SetOutput(a.ErrOut)
		refresh := fs.Bool("refresh", false, "Force refresh telemetry instead of using cache")
		fs.BoolVar(refresh, "r", false, "Alias for --refresh")
		fs.BoolVar(refresh, "f", false, "Alias for --refresh")
		global := fs.Bool("global", false, "Display global account-wide telemetry across all projects")
		fs.BoolVar(global, "g", false, "Alias for --global")
		projectFlag := fs.Bool("project", false, "Force project-scoped telemetry for current directory")
		fs.BoolVar(projectFlag, "p", false, "Alias for --project")
		format := fs.String("format", "table", "Output format: table, markdown, csv, json")
		fs.StringVar(format, "o", "table", "Alias for --format")

		if err := fs.Parse(args[1:]); err != nil {
			return err
		}

		for _, pos := range fs.Args() {
			switch strings.ToLower(pos) {
			case "global", "g":
				*global = true
			case "project", "p":
				*projectFlag = true
			case "markdown", "md", "csv", "json", "table":
				*format = pos
			}
		}

		return a.RunStatsWithOptions(StatsOptions{
			Refresh: *refresh,
			Global:  *global,
			Project: *projectFlag,
			Format:  *format,
		})
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

// runBareRelay handles `torn` with no arguments: when running on an
// interactive terminal it shows the target provider selection screen,
// otherwise it falls back to automatic failover.
func (a *App) runBareRelay() error {
	target, cancelled, err := a.resolveBareTarget()
	if err != nil {
		return err
	}
	if cancelled {
		fmt.Fprintln(a.Out, "Relay aborted, nothing executed.")
		return nil
	}
	return a.RunRelay(target, handoff.Options{})
}

func (a *App) resolveBareTarget() (string, bool, error) {
	if a.Selector != nil {
		return a.Selector()
	}
	return a.selectTargetInteractively()
}

// selectTargetInteractively renders the provider picker on interactive
// terminals. On non-interactive contexts it returns an empty target so the
// caller keeps the automatic failover behavior.
func (a *App) selectTargetInteractively() (string, bool, error) {
	if !interactiveOutput(a.Out) || !isTerminal(os.Stdin) {
		return "", false, nil
	}

	proj, err := project.Detect(a.Cwd)
	if err != nil {
		return "", false, nil
	}
	state, err := a.Store.LoadState(proj)
	if err != nil {
		return "", false, nil
	}
	now := time.Now()

	names := make([]string, 0, 4)
	items := make([]ui.PickerItem, 0, 4)
	stateChanged := false

	// latestSessions tracks the most recent on-disk session per provider to
	// preselect the provider currently most active in this project when torn
	// has no relay state for it yet.
	latestSessions := make(map[string]*provider.Session, 4)
	latestTimes := make(map[string]time.Time, 4)
	for _, p := range a.Registry.All() {
		if sess, sessErr := p.DetectLatestSession(proj.Path); sessErr == nil {
			latestSessions[p.Name()] = sess
			latestTimes[p.Name()] = sess.UpdatedAt
		}
	}
	defaultTarget := pickDefaultTarget(state.ActiveCLI, latestTimes)

	for _, p := range a.Registry.All() {
		names = append(names, p.Name())
		item := ui.PickerItem{
			Label:   p.Name(),
			Default: p.Name() == defaultTarget,
		}
		if p.Name() == defaultTarget {
			item.Hint = "● active"
		} else {
			item.Hint = a.providerPickerHint(p, state, now)
		}

		// Hard quota-exhaustion evidence makes the provider ineligible:
		// first from this project's own latest session (claude), then from
		// account-wide detection for providers implementing QuotaDetector.
		if until := providerQuotaUntil(p, latestSessions[p.Name()], now); until != nil {
			reason := "quota reached, resets " + provider.FormatResetTime(*until, now)
			item.Disabled = true
			item.DisabledWhy = reason
			item.Hint = "✗ " + reason
			a.Cooldown.SetCooldownUntil(p.Name(), state, *until)
			stateChanged = true
		}

		items = append(items, item)
	}

	if stateChanged {
		_ = a.Store.SaveState(proj, state)
	}

	picker := &ui.Picker{
		Out:     a.Out,
		In:      os.Stdin,
		StdinFd: os.Stdin.Fd(),
	}
	choice, selErr := picker.Select("TORN RELAY — select target CLI", items)
	if selErr != nil {
		return "", false, fmt.Errorf("provider selection: %w", selErr)
	}
	if choice < 0 {
		return "", true, nil
	}
	return names[choice], false, nil
}

// providerPickerHint builds the status annotation shown next to a provider.
func (a *App) providerPickerHint(p provider.Provider, state *storage.State, now time.Time) string {
	if _, err := exec.LookPath(p.BinaryName()); err != nil {
		return "○ not installed"
	}
	status, remaining := a.Cooldown.GetStatus(p.Name(), state, now)
	switch status {
	case cooldown.StatusActive:
		return "● active"
	case cooldown.StatusCooldown:
		return "● cooldown " + cooldown.FormatDuration(remaining)
	default:
		return "● ready"
	}
}

// providerQuotaUntil resolves an active quota cooldown for a provider from
// its latest project session or, failing that, from account-wide detection.
func providerQuotaUntil(p provider.Provider, sess *provider.Session, now time.Time) *time.Time {
	if sess != nil {
		if d, err := p.ParseSession(sess.Path); err == nil && d.CooldownUntil != nil && d.CooldownUntil.After(now) {
			return d.CooldownUntil
		}
	}
	if qd, ok := p.(provider.QuotaDetector); ok {
		if until := qd.DetectQuotaCooldown(now); until != nil && until.After(now) {
			return until
		}
	}
	return nil
}

// pickDefaultTarget resolves the preselected provider for the picker:
// torn's recorded active CLI wins; otherwise the provider with the most
// recent session detected in this project is preselected.
func pickDefaultTarget(activeCLI string, latestSessions map[string]time.Time) string {
	if activeCLI != "" {
		return activeCLI
	}

	best := ""
	var bestTime time.Time
	for name, updated := range latestSessions {
		if best == "" || updated.After(bestTime) {
			best = name
			bestTime = updated
		}
	}
	return best
}

// interactiveOutput reports whether w is a character device (a terminal).
func interactiveOutput(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// isTerminal reports whether f is attached to a terminal.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
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
		a.renderDryRun(fromCLI, detectedSession, targetProvider, args, prompt, len(details.Turns), details.CooldownUntil)
		return nil
	}

	// 6. Record cooldown on outgoing provider if switching
	if fromCLI != targetProvider.Name() {
		fromProv, _ := a.Registry.Get(fromCLI)
		if details.CooldownUntil != nil && details.CooldownUntil.After(now) {
			a.Cooldown.SetCooldownUntil(fromCLI, state, *details.CooldownUntil)
		} else {
			cooldownDur := 4 * time.Hour
			if fromProv != nil {
				cooldownDur = fromProv.DefaultCooldown()
			}
			a.Cooldown.SetCooldown(fromCLI, state, cooldownDur, now)
		}
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

func (a *App) renderDryRun(fromCLI string, sess *provider.Session, target provider.Provider, args []string, prompt string, turnsCount int, cooldownUntil *time.Time) {
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
	if cooldownUntil != nil && cooldownUntil.After(time.Now()) {
		fmt.Fprintf(a.Out, "  Cooldown reset:  %s (%s remaining)\n",
			cooldownUntil.Format("3:04pm (MST)"),
			cooldown.FormatDuration(time.Until(*cooldownUntil)),
		)
	}
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

// StatsOptions configures the execution of RunStatsWithOptions.
type StatsOptions struct {
	Refresh bool
	Global  bool
	Project bool
	Format  string
}

// RunStats displays the unified status and quota dashboard.
func (a *App) RunStats(refresh ...bool) error {
	forceRefresh := len(refresh) > 0 && refresh[0]
	return a.RunStatsWithOptions(StatsOptions{
		Refresh: forceRefresh,
		Format:  "table",
	})
}

// RunStatsWithOptions displays or exports the unified status and quota dashboard.
func (a *App) RunStatsWithOptions(opts StatsOptions) error {
	if opts.Format == "" {
		opts.Format = "table"
	}

	proj, err := project.Detect(a.Cwd)
	if err != nil {
		return fmt.Errorf("detecting project: %w", err)
	}

	state, err := a.Store.LoadState(proj)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	now := time.Now()
	cacheDir := filepath.Join(filepath.Dir(a.Store.BaseDir()), "cache")

	projName := filepath.Base(proj.Path)
	projKey := "proj_" + proj.Hash

	hasAIActivity := func(items []stats.ProviderStats) bool {
		for _, ps := range items {
			if ps.SessionsCount > 0 || ps.TotalTokens > 0 {
				return true
			}
		}
		return false
	}

	requestedScope := "auto"
	if opts.Global {
		requestedScope = "global"
	} else if opts.Project {
		requestedScope = "project"
	}

	// 1. Try cache if refresh is not forced (TTL: 60 seconds)
	if !opts.Refresh {
		if requestedScope == "global" {
			if cached, ok := stats.LoadCacheKey(cacheDir, "global", 60*time.Second); ok {
				output, err := stats.Render(cached, opts.Format, stats.DashboardOptions{Scope: "global"})
				if err != nil {
					return err
				}
				fmt.Fprint(a.Out, output)
				return nil
			}
		} else if requestedScope == "project" {
			if cached, ok := stats.LoadCacheKey(cacheDir, projKey, 60*time.Second); ok {
				output, err := stats.Render(cached, opts.Format, stats.DashboardOptions{Scope: "project", ProjectName: projName})
				if err != nil {
					return err
				}
				fmt.Fprint(a.Out, output)
				return nil
			}
		} else {
			// Auto: check project cache first, fallback to global cache
			if cached, ok := stats.LoadCacheKey(cacheDir, projKey, 60*time.Second); ok && hasAIActivity(cached) {
				output, err := stats.Render(cached, opts.Format, stats.DashboardOptions{Scope: "project", ProjectName: projName})
				if err != nil {
					return err
				}
				fmt.Fprint(a.Out, output)
				return nil
			}
			if cached, ok := stats.LoadCacheKey(cacheDir, "global", 60*time.Second); ok {
				output, err := stats.Render(cached, opts.Format, stats.DashboardOptions{Scope: "global"})
				if err != nil {
					return err
				}
				fmt.Fprint(a.Out, output)
				return nil
			}
		}
	}

	// 2. Start braille spinner on a.ErrOut while scanning
	sp := ui.NewSpinner(a.ErrOut, "Collecting telemetry across local providers")
	sp.Start()
	defer sp.Stop()

	// Auto-detect dynamic cooldowns from latest session logs
	stateChanged := false
	for _, p := range a.Registry.All() {
		sess, err := p.DetectLatestSession(proj.Path)
		if err == nil && sess != nil {
			if d, err := p.ParseSession(sess.Path); err == nil && d.CooldownUntil != nil && d.CooldownUntil.After(now) {
				a.Cooldown.SetCooldownUntil(p.Name(), state, *d.CooldownUntil)
				stateChanged = true
			}
		}
	}
	if stateChanged {
		_ = a.Store.SaveState(proj, state)
	}

	collector := stats.NewCollector(a.Registry, a.Cooldown)

	var metrics []stats.ProviderStats
	var dOpts stats.DashboardOptions

	if requestedScope == "global" {
		metrics = collector.Collect(state, now)
		_ = stats.SaveCacheKey(cacheDir, "global", metrics)
		dOpts = stats.DashboardOptions{Scope: "global"}
	} else if requestedScope == "project" {
		metrics = collector.Collect(state, now, proj.Path)
		_ = stats.SaveCacheKey(cacheDir, projKey, metrics)
		dOpts = stats.DashboardOptions{Scope: "project", ProjectName: projName}
	} else {
		// Auto: collect project first; if no AI activity found, fallback to global
		projectMetrics := collector.Collect(state, now, proj.Path)
		if hasAIActivity(projectMetrics) {
			metrics = projectMetrics
			_ = stats.SaveCacheKey(cacheDir, projKey, metrics)
			dOpts = stats.DashboardOptions{Scope: "project", ProjectName: projName}
		} else {
			// Fallback to global
			globalMetrics := collector.Collect(state, now)
			metrics = globalMetrics
			_ = stats.SaveCacheKey(cacheDir, "global", metrics)
			dOpts = stats.DashboardOptions{Scope: "global"}
		}
	}

	// Stop spinner and clean line before rendering
	sp.Stop()

	output, err := stats.Render(metrics, opts.Format, dOpts)
	if err != nil {
		return err
	}
	fmt.Fprint(a.Out, output)
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
  torn [cli] [flags]       Relay to target CLI (interactive selector when no target given)
  torn prep                Copy soft-landing closing prompt to clipboard
  torn stats [flags]       Display or export provider status and quota telemetry
  torn find <query>        Search local sessions across CLIs by keyword
  torn resume [cli]        Restore latest active session into preferred agent
  torn sync                Synchronize global agent rules in $HOME

RELAY FLAGS:
  --drop N                 Prune the last N turns before handoff
  --fresh                  Clear accumulated chain and start fresh task
  --dry-run, -n            Simulate detection and render handoff without executing

STATS FLAGS:
  --global, -g             Display global account-wide telemetry across all projects
  --project, -p            Force project-scoped telemetry for current directory
  --format, -o <fmt>       Output format: table (default), markdown, csv, json
  --refresh, -r            Force refresh telemetry from disk logs instead of cache

EXAMPLES:
  torn                     Interactive selection screen (automatic failover when non-interactive)
  torn --dry-run           Preview generated handoff without executing
  torn stats               Display stats for current directory (or global if no project)
  torn stats --global      Display global account-wide telemetry across all projects
  torn stats --format md   Export status and model metrics as Markdown tables
  torn stats --format csv  Export telemetry in RFC 4180 CSV format
  torn claude              Voluntary return to Claude
  torn codex --drop 2      Relay to Codex dropping the last 2 turns
  torn codex --fresh       Relay to Codex with fresh context for a new feature
`
	fmt.Fprint(a.Out, helpText)
}
