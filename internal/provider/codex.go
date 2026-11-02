package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// CodexProvider manages Codex CLI sessions.
type CodexProvider struct {
	baseDir string
}

// NewCodex creates a CodexProvider. If baseDir is empty, defaults to ~/.codex.
func NewCodex(baseDir string) *CodexProvider {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".codex")
	}
	return &CodexProvider{baseDir: baseDir}
}

func (c *CodexProvider) Name() string       { return "codex" }
func (c *CodexProvider) BinaryName() string { return "codex" }

func (c *CodexProvider) DefaultCooldown() time.Duration {
	return 3 * time.Hour
}

func (c *CodexProvider) FormatExecArgs(prompt string) []string {
	if prompt == "" {
		return []string{"codex"}
	}
	return []string{"codex", prompt}
}

type codexSessionCandidate struct {
	path      string
	sessionID string
	cwd       string
	modTime   time.Time
}

func (c *CodexProvider) DetectLatestSession(targetCwd string) (*Session, error) {
	sessionsDir := filepath.Join(c.baseDir, "sessions")
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("codex sessions directory %q not found", sessionsDir)
	}

	cleanTarget := filepath.Clean(targetCwd)
	var candidates []codexSessionCandidate

	// Walk date subfolders
	err := filepath.WalkDir(sessionsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Inspect session metadata in the first lines to match CWD
		sessionCwd, sessionID := c.inspectSessionMeta(path)
		if sessionCwd != "" && filepath.Clean(sessionCwd) == cleanTarget {
			candidates = append(candidates, codexSessionCandidate{
				path:      path,
				sessionID: sessionID,
				cwd:       sessionCwd,
				modTime:   info.ModTime(),
			})
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walking codex sessions: %w", err)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no codex sessions found for %q", targetCwd)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	latest := candidates[0]
	return &Session{
		ID:        latest.sessionID,
		Provider:  c.Name(),
		Path:      latest.path,
		CWD:       latest.cwd,
		UpdatedAt: latest.modTime,
	}, nil
}

func (c *CodexProvider) inspectSessionMeta(path string) (string, string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// Inspect up to first 20 lines
	linesRead := 0
	for scanner.Scan() && linesRead < 20 {
		linesRead++
		var meta struct {
			Type    string `json:"type"`
			Payload struct {
				Cwd       string `json:"cwd"`
				SessionID string `json:"session_id"`
				ID        string `json:"id"`
			} `json:"payload"`
		}

		if err := json.Unmarshal(scanner.Bytes(), &meta); err == nil {
			if meta.Type == "session_meta" || meta.Type == "turn_context" {
				sid := meta.Payload.SessionID
				if sid == "" {
					sid = meta.Payload.ID
				}
				if meta.Payload.Cwd != "" {
					return meta.Payload.Cwd, sid
				}
			}
		}
	}

	return "", ""
}

func (c *CodexProvider) ParseSession(sessionPath string) (*SessionDetails, error) {
	f, err := os.Open(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("opening codex session %q: %w", sessionPath, err)
	}
	defer f.Close()

	stat, _ := f.Stat()
	details := &SessionDetails{
		Session: Session{
			ID:        strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl"),
			Provider:  c.Name(),
			Path:      sessionPath,
			UpdatedAt: stat.ModTime(),
		},
		Turns: make([]Turn, 0),
	}

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry struct {
			Type    string `json:"type"`
			Payload struct {
				Role    string `json:"role"`
				Message string `json:"message"`
				Text    string `json:"text"`
				Content string `json:"content"`
			} `json:"payload"`
		}

		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		text := entry.Payload.Text
		if text == "" {
			text = entry.Payload.Content
		}
		if text == "" {
			text = entry.Payload.Message
		}

		role := entry.Payload.Role
		if role == "" {
			if entry.Type == "user_message" || entry.Type == "user" {
				role = "user"
			} else if entry.Type == "assistant_message" || entry.Type == "response_item" {
				role = "assistant"
			}
		}

		if text != "" && role != "" {
			details.Turns = append(details.Turns, Turn{
				Role:      role,
				Text:      text,
				Timestamp: stat.ModTime(),
			})
			if role == "user" {
				details.LastUserMessage = text
			} else {
				details.LastAssistantMessage = text
			}
		}
	}

	return details, nil
}
