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
	// Explicit 10 MB buffer to prevent failure on large tool outputs
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}

		lineType, _ := raw["type"].(string)
		msgMap, _ := raw["message"].(map[string]any)
		role, _ := raw["role"].(string)

		if lineType == "user" || (msgMap != nil && msgMap["role"] == "user") || role == "user" {
			var rawContent any
			if msgMap != nil {
				rawContent = msgMap["content"]
			}
			if rawContent == nil {
				rawContent = raw["display"]
			}
			if rawContent == nil {
				rawContent = raw["content"]
			}

			extracted := ParseContentBlock(rawContent)
			if extracted.Text != "" {
				details.Turns = append(details.Turns, Turn{
					Role:      "user",
					Text:      extracted.Text,
					Timestamp: stat.ModTime(),
				})
				details.LastUserMessage = extracted.Text
			}
		} else if lineType == "assistant" || (msgMap != nil && msgMap["role"] == "assistant") || role == "assistant" {
			var rawContent any
			if msgMap != nil {
				rawContent = msgMap["content"]
			}
			if rawContent == nil {
				rawContent = raw["content"]
			}

			extracted := ParseContentBlock(rawContent)

			// Extract tokens
			var turnTokens int
			if msgMap != nil {
				if usage, ok := msgMap["usage"].(map[string]any); ok {
					inTok := toInt(usage["input_tokens"]) + toInt(usage["cache_read_input_tokens"]) + toInt(usage["cache_creation_input_tokens"])
					outTok := toInt(usage["output_tokens"])
					turnTokens = inTok + outTok
					details.TotalTokens += turnTokens
				}
			}

			if extracted.Text != "" || len(extracted.Tools) > 0 {
				details.Turns = append(details.Turns, Turn{
					Role:      "assistant",
					Text:      extracted.Text,
					Tools:     extracted.Tools,
					Tokens:    turnTokens,
					Timestamp: stat.ModTime(),
				})
				if extracted.Text != "" {
					details.LastAssistantMessage = extracted.Text
				}
			}
		}
	}

	return details, scanner.Err()
}

func (c *ClaudeProvider) CollectUsage(now time.Time) (UsageMetrics, error) {
	var metrics UsageMetrics
	projectsDir := filepath.Join(c.baseDir, "projects")

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return metrics, nil
	}

	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfWeek := startOfDay.AddDate(0, 0, -7)

	for _, pEntry := range entries {
		if !pEntry.IsDir() {
			continue
		}
		pDir := filepath.Join(projectsDir, pEntry.Name())
		files, err := os.ReadDir(pDir)
		if err != nil {
			continue
		}

		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			metrics.TotalSessions++
			fInfo, err := f.Info()
			if err != nil || fInfo.ModTime().Before(startOfWeek) {
				continue
			}

			details, err := c.ParseSession(filepath.Join(pDir, f.Name()))
			if err == nil && details.TotalTokens > 0 {
				metrics.TokensThisWeek += details.TotalTokens
				if fInfo.ModTime().After(startOfDay) {
					metrics.TokensToday += details.TotalTokens
				}
			}
		}
	}

	return metrics, nil
}

func toInt(v any) int {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}
