package clipboard

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
)

// Writer defines the capability to write text to system clipboard.
type Writer func(text string) error

// Copy copies text to the system clipboard using the platform-native utility.
func Copy(text string) error {
	return DefaultWriter(text)
}

// DefaultWriter writes text using pbcopy (macOS) or wl-copy/xclip (Linux).
func DefaultWriter(text string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		if path, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command(path)
		} else if path, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command(path, "-selection", "clipboard")
		} else if path, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command(path, "--clipboard", "--input")
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
