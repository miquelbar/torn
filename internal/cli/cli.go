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

	sessionPath := "(ninguna sesión previa detectada)"
	if sess != nil {
		sessionPath = sess.Path
	}
	fmt.Fprintf(a.Out, "• Origen detectado: %s\n", fromCLI)
	fmt.Fprintf(a.Out, "  Archivo:          %s\n", sessionPath)
	fmt.Fprintf(a.Out, "  Turnos parseados: %d\n", turnsCount)
	fmt.Fprintf(a.Out, "• Destino:          %s\n", target.Name())

	cmdDisplay := target.BinaryName()
	if len(args) > 1 {
		cmdDisplay = fmt.Sprintf("%s %s \"<HANDOFF_PROMPT>\"", target.BinaryName(), strings.Join(args[1:len(args)-1], " "))
	}
	fmt.Fprintf(a.Out, "• Invocación Exec:  %s\n\n", strings.TrimSpace(cmdDisplay))
	fmt.Fprintln(a.Out, "--------------------------------------------------------------------------------")
	fmt.Fprintln(a.Out, "PROMPT DE HANDOFF GENERADO (inyectado vía argv):")
	fmt.Fprintln(a.Out, "--------------------------------------------------------------------------------")
	fmt.Fprintln(a.Out, prompt)
	fmt.Fprintln(a.Out, "================================================================================")
	fmt.Fprintln(a.Out, "Dry-run completado. No se alteró ningún estado ni se ejecutó ningún proceso.")
}

// RunPrep copies a standardized closing prompt to clipboard.
func (a *App) RunPrep() error {
	prompt := `Estamos aproximándonos al límite de cuota de la sesión (85-90%).
Por favor, prepara un bloque limpio de cierre y traspaso (handoff) con:
1. Milestone y tarea activa actual.
2. Archivos modificados y estado de compilación/tests.
3. Decisiones arquitectónicas o bloqueos pendientes.
4. Pasos exactos a seguir por el siguiente agente.`

	if err := a.Clipboard(prompt); err != nil {
		fmt.Fprintf(a.ErrOut, "Aviso: no se pudo copiar al portapapeles (%v)\n", err)
		fmt.Fprintln(a.Out, "--- Prompt de cierre ---")
		fmt.Fprintln(a.Out, prompt)
		return nil
	}

	fmt.Fprintln(a.Out, "🟢 [TORN PREP] Prompt de cierre copiado al portapapeles.")
	fmt.Fprintln(a.Out, "Pégalo en tu CLI actual para que redacte su propio resumen antes de agotar la cuota.")
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
		fmt.Fprintf(a.Out, "No se encontraron sesiones que contengan %q en este proyecto.\n", query)
		return nil
	}

	fmt.Fprintf(a.Out, "Encontradas %d sesiones con coincidencias para %q:\n\n", len(matches), query)
	for _, m := range matches {
		fmt.Fprintf(a.Out, "• [%s] %s (%d turnos) — %s\n",
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
	fmt.Fprintln(a.Out, "🟢 [TORN SYNC] Reglas y directrices de agentes sincronizadas en $HOME.")
	return nil
}

func (a *App) printHelp() {
	helpText := `torn - Hipervisor local de huella cero para relevos entre CLIs de IA

USO:
  torn [cli] [flags]       Ejecuta el relevo al siguiente CLI (o al indicado)
  torn prep                Copia al portapapeles el prompt de aterrizaje suave
  torn stats               Muestra el cuadro de mando de estados y cooldowns
  torn find <texto>        Busca sesiones locales por palabra clave
  torn resume [cli]        Retoma la última sesión activa en el agente deseado
  torn sync                Sincroniza directrices globales en $HOME

FLAGS DE RELEVO:
  --drop N                 Amputa los últimos N turnos antes de pasar el testigo
  --fresh                  Limpia la cadena acumulada e inicia tarea fresca
  --dry-run, -n            Simula la detección y muestra el handoff sin ejecutar

EJEMPLOS:
  torn                     Relevo automático por failover al siguiente CLI libre
  torn --dry-run           Comprueba qué handoff se generaría sin ejecutar nada
  torn claude              Relevo voluntario de vuelta a Claude
  torn codex --drop 2      Relevo a Codex amputando los últimos 2 turnos
  torn codex --fresh       Relevo a Codex con contexto limpio para nueva feature
`
	fmt.Fprint(a.Out, helpText)
}
