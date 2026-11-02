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

// VibeProvider manages Vibe CLI sessions.
type VibeProvider struct {
	baseDir string
}

// NewVibe creates a VibeProvider. If baseDir is empty, defaults to ~/.vibe.
func NewVibe(baseDir string) *VibeProvider {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".vibe")
	}
	return &VibeProvider{baseDir: baseDir}
}

func (v *VibeProvider) Name() string       { return "vibe" }
func (v *VibeProvider) BinaryName() string { return "vibe" }

func (v *VibeProvider) DefaultCooldown() time.Duration {
	return 3 * time.Hour
}

func (v *VibeProvider) FormatExecArgs(prompt string) []string {
	if prompt == "" {
		return []string{"vibe"}
	}
	return []string{"vibe", prompt}
}

type vibeIndexEntry struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
	UpdatedAt string `json:"updated_at"`
	StartTime string `json:"start_time"`
}

func (v *VibeProvider) DetectLatestSession(targetCwd string) (*Session, error) {
	indexPath := filepath.Join(v.baseDir, "logs", "session", ".session_index.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("vibe session index not found at %q", indexPath)
		}
		return nil, fmt.Errorf("reading vibe session index: %w", err)
	}

	var index map[string]vibeIndexEntry
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("decoding vibe session index: %w", err)
	}

	cleanTarget := filepath.Clean(targetCwd)
	type matched struct {
		key       string
		entry     vibeIndexEntry
		updatedAt time.Time
	}

	var matches []matched
	for key, entry := range index {
		if filepath.Clean(entry.CWD) == cleanTarget {
			parsedTime, err := time.Parse(time.RFC3339Nano, entry.UpdatedAt)
			if err != nil {
				parsedTime, _ = time.Parse(time.RFC3339, entry.UpdatedAt)
			}
			matches = append(matches, matched{
				key:       key,
				entry:     entry,
				updatedAt: parsedTime,
			})
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no vibe sessions found for %q", targetCwd)
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].updatedAt.After(matches[j].updatedAt)
	})

	latest := matches[0]
	sessionPath := filepath.Join(v.baseDir, "logs", "session", latest.key)

	return &Session{
		ID:        latest.entry.SessionID,
		Provider:  v.Name(),
		Path:      sessionPath,
		CWD:       latest.entry.CWD,
		UpdatedAt: latest.updatedAt,
	}, nil
}

func (v *VibeProvider) ParseSession(sessionPath string) (*SessionDetails, error) {
	stat, err := os.Stat(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("accessing vibe session %q: %w", sessionPath, err)
	}

	details := &SessionDetails{
		Session: Session{
			ID:        filepath.Base(sessionPath),
			Provider:  v.Name(),
			Path:      sessionPath,
			UpdatedAt: stat.ModTime(),
		},
		Turns: make([]Turn, 0),
	}

	messagesFile := filepath.Join(sessionPath, "messages.jsonl")
	if !stat.IsDir() {
		messagesFile = sessionPath
	}

	f, err := os.Open(messagesFile)
	if err != nil {
		return details, nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var row struct {
			Role             string `json:"role"`
			Content          any    `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		}

		if err := json.Unmarshal(line, &row); err != nil {
			continue
		}

		extracted := ParseContentBlock(row.Content)

		var tools []string
		for _, tc := range row.ToolCalls {
			toolStr := tc.Function.Name
			argSummary := extractArgSummary(tc.Function.Arguments)
			if argSummary != "" {
				toolStr = fmt.Sprintf("%s(%s)", tc.Function.Name, argSummary)
			}
			tools = append(tools, toolStr)
		}

		if row.Role == "user" && extracted.Text != "" {
			details.Turns = append(details.Turns, Turn{
				Role:      "user",
				Text:      extracted.Text,
				Timestamp: stat.ModTime(),
			})
			details.LastUserMessage = extracted.Text
		} else if row.Role == "assistant" {
			text := extracted.Text
			if text == "" && row.ReasoningContent != "" {
				text = row.ReasoningContent
			}
			if text != "" || len(tools) > 0 {
				details.Turns = append(details.Turns, Turn{
					Role:      "assistant",
					Text:      text,
					Tools:     tools,
					Timestamp: stat.ModTime(),
				})
				if text != "" {
					details.LastAssistantMessage = text
				}
			}
		}
	}

	return details, scanner.Err()
}

func (v *VibeProvider) CollectUsage(now time.Time) (UsageMetrics, error) {
	var metrics UsageMetrics
	sessionDir := filepath.Join(v.baseDir, "logs", "session")
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return metrics, nil
	}

	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfWeek := startOfDay.AddDate(0, 0, -7)

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		metaPath := filepath.Join(sessionDir, entry.Name(), "meta.json")
		metaData, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}

		metrics.TotalSessions++
		var meta struct {
			StartTime string `json:"start_time"`
			Stats     struct {
				TotalTokens int64 `json:"session_total_llm_tokens"`
			} `json:"stats"`
		}
		if err := json.Unmarshal(metaData, &meta); err == nil {
			tokens := meta.Stats.TotalTokens
			metrics.TotalTokens += tokens

			var t time.Time
			if meta.StartTime != "" {
				t, _ = time.Parse(time.RFC3339, meta.StartTime)
			}
			if t.IsZero() {
				if info, err := entry.Info(); err == nil {
					t = info.ModTime()
				}
			}

			if t.After(startOfWeek) {
				metrics.TokensThisWeek += tokens
			}
			if t.After(startOfDay) {
				metrics.TokensToday += tokens
			}
		}
	}

	return metrics, nil
}
