package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// Writer defines the capability to write text to system clipboard.
type Writer func(text string) error

// Copy copies text to the system clipboard using the platform-native utility.
func Copy(text string) error {
	return DefaultWriter(text)
}

// DefaultWriter writes text using pbcopy (macOS) or wl-copy/xclip (Linux) with a timeout.
func DefaultWriter(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "pbcopy")
	case "linux":
		if path, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.CommandContext(ctx, path)
		} else if path, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.CommandContext(ctx, path, "-selection", "clipboard")
		} else if path, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.CommandContext(ctx, path, "--clipboard", "--input")
		} else {
			return fmt.Errorf("no clipboard utility found (install wl-copy or xclip)")
		}
	default:
		return fmt.Errorf("clipboard unsupported on OS %q", runtime.GOOS)
	}

	cmd.Stdin = bytes.NewReader([]byte(text))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running clipboard command: %w", err)
	}
	return nil
}
