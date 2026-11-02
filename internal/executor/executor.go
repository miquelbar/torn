package executor

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// ExecRunner describes the signature of a process replacement execution.
type ExecRunner func(binaryPath string, argv []string, env []string) error

// SyscallExecRunner implements real process replacement using syscall.Exec.
func SyscallExecRunner(binaryPath string, argv []string, env []string) error {
	return syscall.Exec(binaryPath, argv, env)
}

// Executor handles launching CLI processes with injected argv.
type Executor struct {
	runner ExecRunner
}

// New creates an Executor with the provided ExecRunner.
func New(runner ExecRunner) *Executor {
	if runner == nil {
		runner = SyscallExecRunner
	}
	return &Executor{runner: runner}
}

// Execute looks up the binary, sets working directory, and replaces the current process.
func (e *Executor) Execute(binaryName string, args []string, cwd string) error {
	binaryPath, err := exec.LookPath(binaryName)
	if err != nil {
		return fmt.Errorf("CLI binary %q not found in PATH: %w", binaryName, err)
	}

	if cwd != "" {
		if err := os.Chdir(cwd); err != nil {
			return fmt.Errorf("changing directory to %q: %w", cwd, err)
		}
	}

	// argv[0] must be the binary name/path
	argv := append([]string{binaryName}, args...)
	env := os.Environ()

	return e.runner(binaryPath, argv, env)
}
