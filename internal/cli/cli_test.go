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
	if !strings.Contains(out.String(), "USO:") {
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

	if !strings.Contains(out.String(), "[TORN PREP] Prompt de cierre copiado") {
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

	if err := app.Run([]string{"stats"}); err != nil {
		t.Fatalf("stats failed: %v", err)
	}

	if !strings.Contains(out.String(), "TORN HYPERVISOR STATS") {
		t.Errorf("expected stats table header, got: %s", out.String())
	}
}

func TestCLISyncCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	app, err := NewApp(&out, &errOut, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	if err := app.Run([]string{"sync"}); err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	if !strings.Contains(out.String(), "[TORN SYNC]") {
		t.Errorf("expected sync output: %s", out.String())
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

	// Register a mock provider that doesn't rely on global directories
	// In this case, claude or codex binary can be simulated
	app.Executor = executor.New(mockRunner)

	// Run relay with mock binary that exists in system path, e.g. "sh" or "git"
	err = app.RunRelay("claude", struct {
		DropLastNTurns int
		Fresh          bool
	}{Fresh: true})

	// Claude binary is installed on user machine: /Users/miquel/.local/bin/claude
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
