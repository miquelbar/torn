package cli

import (
	"bytes"
	"strings"
	"testing"

	"torn/internal/executor"
)

func TestCLIVersionAndHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	app, err := NewApp(&out, &errOut, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	if err := app.Run([]string{"version"}); err != nil {
		t.Fatalf("version failed: %v", err)
	}
	if !strings.Contains(out.String(), "torn v0.1.0") {
		t.Errorf("version output unexpected: %s", out.String())
	}

	out.Reset()
	if err := app.Run([]string{"help"}); err != nil {
		t.Fatalf("help failed: %v", err)
	}
	if !strings.Contains(out.String(), "USAGE:") {
		t.Errorf("help output unexpected: %s", out.String())
	}
}

func TestCLIPrepCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	var copiedText string

	mockClip := func(text string) error {
		copiedText = text
		return nil
	}

	app, err := NewApp(&out, &errOut, t.TempDir(), nil, mockClip)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	if err := app.Run([]string{"prep"}); err != nil {
		t.Fatalf("prep failed: %v", err)
	}

	if !strings.Contains(out.String(), "[TORN PREP] Handoff summary prompt copied") {
		t.Errorf("expected confirmation message in output: %s", out.String())
	}
	if !strings.Contains(copiedText, "85-90%") {
		t.Errorf("expected copied text to contain threshold: %s", copiedText)
	}
}

func TestCLIStatsCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	app, err := NewApp(&out, &errOut, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	// 1. Initial run: populates cache
	if err := app.Run([]string{"stats"}); err != nil {
		t.Fatalf("stats failed: %v", err)
	}

	if !strings.Contains(out.String(), "TORN HYPERVISOR STATS") {
		t.Errorf("expected stats table header, got: %s", out.String())
	}

	// 2. Second run: served from cache
	out.Reset()
	if err := app.Run([]string{"stats"}); err != nil {
		t.Fatalf("cached stats failed: %v", err)
	}
	if !strings.Contains(out.String(), "TORN HYPERVISOR STATS") {
		t.Errorf("expected cached stats table header, got: %s", out.String())
	}

	// 3. Forced refresh run
	out.Reset()
	if err := app.Run([]string{"stats", "--refresh"}); err != nil {
		t.Fatalf("forced refresh stats failed: %v", err)
	}
	if !strings.Contains(out.String(), "TORN HYPERVISOR STATS") {
		t.Errorf("expected refreshed stats table header, got: %s", out.String())
	}

	// 4. Export as Markdown
	out.Reset()
	if err := app.Run([]string{"stats", "--format", "markdown"}); err != nil {
		t.Fatalf("stats markdown failed: %v", err)
	}
	if !strings.Contains(out.String(), "# Torn Hypervisor Stats") || !strings.Contains(out.String(), "| Provider |") {
		t.Errorf("expected markdown tables, got:\n%s", out.String())
	}

	// 5. Export as CSV via -o flag
	out.Reset()
	if err := app.Run([]string{"stats", "-o", "csv"}); err != nil {
		t.Fatalf("stats csv failed: %v", err)
	}
	if !strings.Contains(out.String(), "Provider,Status,Cooldown") {
		t.Errorf("expected CSV headers, got:\n%s", out.String())
	}

	// 6. Export as JSON
	out.Reset()
	if err := app.Run([]string{"stats", "--format", "json"}); err != nil {
		t.Fatalf("stats json failed: %v", err)
	}
	if !strings.Contains(out.String(), `"provider"`) && !strings.Contains(out.String(), `"name"`) {
		t.Errorf("expected JSON structure, got:\n%s", out.String())
	}

	// 7. Positional format alias
	out.Reset()
	if err := app.Run([]string{"stats", "md"}); err != nil {
		t.Fatalf("stats md positional failed: %v", err)
	}
	if !strings.Contains(out.String(), "# Torn Hypervisor Stats") {
		t.Errorf("expected markdown from positional argument, got:\n%s", out.String())
	}

	// 8. Unsupported format error
	if err := app.Run([]string{"stats", "--format", "invalid"}); err == nil {
		t.Errorf("expected error for unsupported format 'invalid', got nil")
	}
}

func TestCLIDryRun(t *testing.T) {
	var out, errOut bytes.Buffer
	executed := false
	mockRunner := func(binaryPath string, argv []string, env []string) error {
		executed = true
		return nil
	}

	app, err := NewApp(&out, &errOut, t.TempDir(), mockRunner, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	// Run with --dry-run
	err = app.Run([]string{"--dry-run", "codex"})
	if err != nil {
		t.Fatalf("Run --dry-run failed: %v", err)
	}

	if executed {
		t.Fatalf("expected executor NOT to be called under --dry-run")
	}

	output := out.String()
	if !strings.Contains(output, "TORN RELAY DRY-RUN") {
		t.Errorf("expected dry-run header in output: %s", output)
	}
	if !strings.Contains(output, "Target:          codex") {
		t.Errorf("expected target codex in output: %s", output)
	}
	if !strings.Contains(output, "GENERATED HANDOFF PROMPT") {
		t.Errorf("expected handoff prompt section: %s", output)
	}
}

func TestCLIRelayDispatchWithMockExecutor(t *testing.T) {
	var out, errOut bytes.Buffer
	var executedBinary string
	var executedArgs []string

	mockRunner := func(binaryPath string, argv []string, env []string) error {
		executedBinary = binaryPath
		executedArgs = argv
		return nil
	}

	tempStore := t.TempDir()
	app, err := NewApp(&out, &errOut, tempStore, mockRunner, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	app.Executor = executor.New(mockRunner)

	err = app.RunRelay("claude", struct {
		DropLastNTurns int
		Fresh          bool
		DryRun         bool
	}{Fresh: true})

	if err != nil {
		t.Fatalf("RunRelay failed: %v", err)
	}

	if !strings.HasSuffix(executedBinary, "claude") {
		t.Errorf("expected claude binary execution, got: %s", executedBinary)
	}
	if len(executedArgs) < 2 {
		t.Fatalf("expected injected argv[1], got: %v", executedArgs)
	}
	if !strings.Contains(executedArgs[1], "TORN RELAY HANDOFF") {
		t.Errorf("expected injected handoff prompt in argv[1]")
	}
}
