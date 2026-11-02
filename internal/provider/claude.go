package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ClaudeProvider manages Claude CLI sessions.
type ClaudeProvider struct {
	baseDir string
}

// NewClaude creates a ClaudeProvider. If baseDir is empty, defaults to ~/.claude.
func NewClaude(baseDir string) *ClaudeProvider {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".claude")
	}
	return &ClaudeProvider{baseDir: baseDir}
}

func (c *ClaudeProvider) Name() string       { return "claude" }
func (c *ClaudeProvider) BinaryName() string { return "claude" }

func (c *ClaudeProvider) DefaultCooldown() time.Duration {
	return 5 * time.Hour
}

func (c *ClaudeProvider) FormatExecArgs(prompt string) []string {
	if prompt == "" {
		return []string{"claude"}
	}
	return []string{"claude", prompt}
}

// PathToSlug converts an absolute path to Claude's directory slug format.
// e.g., /Users/test/Projects/app -> -Users-test-Projects-app
func (c *ClaudeProvider) PathToSlug(p string) string {
	clean := filepath.Clean(p)
	return strings.ReplaceAll(clean, string(filepath.Separator), "-")
}

func (c *ClaudeProvider) DetectLatestSession(cwd string) (*Session, error) {
	slug := c.PathToSlug(cwd)
	projectDir := filepath.Join(c.baseDir, "projects", slug)

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no claude project sessions found for %q", cwd)
		}
		return nil, fmt.Errorf("reading claude project dir %q: %w", projectDir, err)
	}

	type fileWithTime struct {
		name    string
		path    string
		modTime time.Time
	}

	var candidates []fileWithTime
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, fileWithTime{
			name:    entry.Name(),
			path:    filepath.Join(projectDir, entry.Name()),
			modTime: info.ModTime(),
		})
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no claude session logs found for %q", cwd)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	latest := candidates[0]
	sessionID := strings.TrimSuffix(latest.name, ".jsonl")

	return &Session{
		ID:        sessionID,
		Provider:  c.Name(),
		Path:      latest.path,
		CWD:       cwd,
		UpdatedAt: latest.modTime,
	}, nil
}

func (c *ClaudeProvider) ParseSession(sessionPath string) (*SessionDetails, error) {
	file, err := os.Open(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("opening claude session %q: %w", sessionPath, err)
	}
	defer file.Close()

	stat, _ := file.Stat()
	details := &SessionDetails{
		Session: Session{
			ID:        strings.TrimSuffix(filepath.Base(sessionPath), ".jsonl"),
			Provider:  c.Name(),
			Path:      sessionPath,
			UpdatedAt: stat.ModTime(),
		},
		Turns: make([]Turn, 0),
	}

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var raw map[string]interface{}
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}

		// Claude jsonl typically records role/message
		role, _ := raw["role"].(string)
		content, _ := raw["content"].(string)

		if content == "" {
			// Check if message is nested
			if msg, ok := raw["message"].(map[string]interface{}); ok {
				if r, ok := msg["role"].(string); ok {
					role = r
				}
				if c, ok := msg["content"].(string); ok {
					content = c
				}
			}
		}

		if content == "" && raw["display"] != nil {
			content, _ = raw["display"].(string)
			if role == "" {
				role = "user"
			}
		}

		if content != "" {
			turn := Turn{
				Role:      role,
				Text:      content,
				Timestamp: stat.ModTime(),
			}
			details.Turns = append(details.Turns, turn)
			if role == "user" {
				details.LastUserMessage = content
			} else {
				details.LastAssistantMessage = content
			}
		}
	}

	return details, nil
}
