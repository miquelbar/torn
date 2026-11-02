package executor

import (
	"os"
	"strings"
	"testing"
)

func TestExecutorWithMockRunner(t *testing.T) {
	var capturedPath string
	var capturedArgv []string
	var capturedEnv []string

	mockRunner := func(binaryPath string, argv []string, env []string) error {
		capturedPath = binaryPath
		capturedArgv = argv
		capturedEnv = env
		return nil
	}

	execInstance := New(mockRunner)

	// Test with a binary guaranteed on system path, e.g. "sh" or "git"
	err := execInstance.Execute("git", []string{"status", "--short"}, "")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.HasSuffix(capturedPath, "git") {
		t.Errorf("expected binary path ending in git, got %q", capturedPath)
	}

	if len(capturedArgv) != 3 {
		t.Fatalf("expected 3 argv elements, got %d", len(capturedArgv))
	}
	if capturedArgv[0] != "git" || capturedArgv[1] != "status" || capturedArgv[2] != "--short" {
		t.Errorf("unexpected argv: %v", capturedArgv)
	}

	if len(capturedEnv) != len(os.Environ()) {
		t.Errorf("expected env length %d, got %d", len(os.Environ()), len(capturedEnv))
	}
}

func TestExecutorBinaryNotFound(t *testing.T) {
	execInstance := New(nil)
	err := execInstance.Execute("completely_non_existent_binary_xyz", nil, "")
	if err == nil {
		t.Fatal("expected error for non-existent binary, got nil")
	}
}
