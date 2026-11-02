package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Info represents metadata for a target project workspace.
type Info struct {
	Path    string `json:"path"`
	Hash    string `json:"hash"`
	GitHead string `json:"git_head"`
	IsGit   bool   `json:"is_git"`
}

// GitRunner defines the capability to execute git commands in a directory.
type GitRunner func(dir string, args ...string) (string, error)

// DefaultGitRunner executes native git commands.
func DefaultGitRunner(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (stderr: %s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Detect inspects the directory at cwd and returns its project Info.
func Detect(cwd string) (*Info, error) {
	return DetectWithGit(cwd, DefaultGitRunner)
}

// DetectWithGit allows injecting a GitRunner for deterministic testing.
func DetectWithGit(cwd string, git GitRunner) (*Info, error) {
	absPath, err := filepath.Abs(cwd)
	if err != nil {
		return nil, fmt.Errorf("resolving absolute path for %q: %w", cwd, err)
	}

	evalPath, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		absPath = evalPath
	}

	stat, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat path %q: %w", absPath, err)
	}
	if !stat.IsDir() {
		return nil, fmt.Errorf("path %q is not a directory", absPath)
	}

	hash := HashPath(absPath)

	info := &Info{
		Path: absPath,
		Hash: hash,
	}

	if git != nil {
		head, err := git(absPath, "rev-parse", "HEAD")
		if err == nil && head != "" {
			info.IsGit = true
			info.GitHead = head
		}
	}

	return info, nil
}

// HashPath computes a stable 16-character hexadecimal hash of an absolute path.
func HashPath(path string) string {
	clean := filepath.Clean(path)
	sum := sha256.Sum256([]byte(clean))
	return hex.EncodeToString(sum[:])[:16]
}
