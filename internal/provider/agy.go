package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AgyProvider manages Antigravity CLI sessions.
type AgyProvider struct {
	baseDir string
}

// NewAgy creates an AgyProvider. If baseDir is empty, defaults to ~/.gemini/antigravity-cli.
func NewAgy(baseDir string) *AgyProvider {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".gemini", "antigravity-cli")
	}
	return &AgyProvider{baseDir: baseDir}
}

func (a *AgyProvider) Name() string       { return "agy" }
func (a *AgyProvider) BinaryName() string { return "agy" }

func (a *AgyProvider) DefaultCooldown() time.Duration {
	return 4 * time.Hour
}

func (a *AgyProvider) FormatExecArgs(prompt string) []string {
	if prompt == "" {
		return []string{"agy"}
	}
	return []string{"agy", prompt}
}

func (a *AgyProvider) DetectLatestSession(targetCwd string) (*Session, error) {
	historyPath := filepath.Join(a.baseDir, "history.jsonl")
	f, err := os.Open(historyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("agy history file not found at %q", historyPath)
		}
		return nil, fmt.Errorf("reading agy history: %w", err)
	}
	defer f.Close()

	cleanTarget := filepath.Clean(targetCwd)
	var lastMatch *Session

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var item struct {
			SessionID string `json:"session_id"`
			Cwd       string `json:"cwd"`
			Project   string `json:"project"`
			Timestamp int64  `json:"timestamp"`
		}

		if err := json.Unmarshal(scanner.Bytes(), &item); err == nil {
			cwd := item.Cwd
			if cwd == "" {
				cwd = item.Project
			}
			if filepath.Clean(cwd) == cleanTarget && item.SessionID != "" {
				updated := time.Unix(item.Timestamp/1000, 0)
				if item.Timestamp < 10000000000 {
					updated = time.Unix(item.Timestamp, 0)
				}
				lastMatch = &Session{
					ID:        item.SessionID,
					Provider:  a.Name(),
					Path:      filepath.Join(a.baseDir, "conversations", item.SessionID),
					CWD:       cwd,
					UpdatedAt: updated,
				}
			}
		}
	}

	if lastMatch == nil {
		return nil, fmt.Errorf("no agy sessions found for %q", targetCwd)
	}

	return lastMatch, nil
}

func (a *AgyProvider) ParseSession(sessionPath string) (*SessionDetails, error) {
	stat, err := os.Stat(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("accessing agy session %q: %w", sessionPath, err)
	}

	details := &SessionDetails{
		Session: Session{
			ID:        filepath.Base(sessionPath),
			Provider:  a.Name(),
			Path:      sessionPath,
			UpdatedAt: stat.ModTime(),
		},
		Turns: make([]Turn, 0),
	}

	return details, nil
}
