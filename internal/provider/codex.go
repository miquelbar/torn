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

	err := filepath.WalkDir(sessionsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

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
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	linesRead := 0
	for scanner.Scan() && linesRead < 30 {
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
		payload, _ := raw["payload"].(map[string]any)
		if payload == nil {
			continue
		}

		// 1. Check response_item
		if lineType == "response_item" {
			role, _ := payload["role"].(string)
			extracted := ParseContentBlock(payload["content"])
			if extracted.Text == "" {
				extracted = ParseContentBlock(payload["text"])
			}

			if extracted.Text != "" || len(extracted.Tools) > 0 {
				details.Turns = append(details.Turns, Turn{
					Role:      role,
					Text:      extracted.Text,
					Tools:     extracted.Tools,
					Timestamp: stat.ModTime(),
				})
				if role == "user" && extracted.Text != "" {
					details.LastUserMessage = extracted.Text
				} else if role == "assistant" && extracted.Text != "" {
					details.LastAssistantMessage = extracted.Text
				}
			}
		} else if lineType == "event_msg" {
			// Check item_completed in event_msg
			item, _ := payload["item"].(map[string]any)
			if item != nil {
				itemType, _ := item["type"].(string)
				switch itemType {
				case "UserMessage":
					extracted := ParseContentBlock(item["content"])
					if extracted.Text != "" {
						details.Turns = append(details.Turns, Turn{
							Role:      "user",
							Text:      extracted.Text,
							Timestamp: stat.ModTime(),
						})
						details.LastUserMessage = extracted.Text
					}
				case "AgentMessage":
					extracted := ParseContentBlock(item["content"])
					if extracted.Text != "" {
						details.Turns = append(details.Turns, Turn{
							Role:      "assistant",
							Text:      extracted.Text,
							Timestamp: stat.ModTime(),
						})
						details.LastAssistantMessage = extracted.Text
					}
				case "CommandExecution":
					cmdList, _ := item["command"].([]any)
					var parts []string
					for _, c := range cmdList {
						if s, ok := c.(string); ok {
							parts = append(parts, s)
						}
					}
					if len(parts) > 0 {
						details.Turns = append(details.Turns, Turn{
							Role:      "assistant",
							Tools:     []string{fmt.Sprintf("exec(%s)", strings.Join(parts, " "))},
							Timestamp: stat.ModTime(),
						})
					}
				}
			}
		}
	}

	return details, scanner.Err()
}

func (c *CodexProvider) CollectUsage(now time.Time) (UsageMetrics, error) {
	var metrics UsageMetrics

	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfWeek := startOfDay.AddDate(0, 0, -7)

	sessionsDir := filepath.Join(c.baseDir, "sessions")
	_ = filepath.WalkDir(sessionsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if !strings.HasPrefix(d.Name(), "rollout-") {
			return nil
		}

		metrics.TotalSessions++
		info, err := d.Info()
		if err != nil {
			return nil
		}

		realTokens := c.scanRolloutTokens(path)
		metrics.TotalTokens += realTokens
		if info.ModTime().After(startOfWeek) {
			metrics.TokensThisWeek += realTokens
		}
		if info.ModTime().After(startOfDay) {
			metrics.TokensToday += realTokens
		}
		return nil
	})

	return metrics, nil
}

func (c *CodexProvider) scanRolloutTokens(path string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	var lastIn, lastCached, lastOut int64
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if !strings.Contains(string(line), `"total_token_usage"`) {
			continue
		}

		var raw struct {
			Payload struct {
				Info struct {
					TotalTokenUsage struct {
						InputTokens       int64 `json:"input_tokens"`
						CachedInputTokens int64 `json:"cached_input_tokens"`
						OutputTokens      int64 `json:"output_tokens"`
					} `json:"total_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &raw); err == nil {
			u := raw.Payload.Info.TotalTokenUsage
			if u.InputTokens > 0 || u.OutputTokens > 0 {
				lastIn = u.InputTokens
				lastCached = u.CachedInputTokens
				lastOut = u.OutputTokens
			}
		}
	}

	realIn := lastIn - lastCached
	if realIn < 0 {
		realIn = 0
	}
	return realIn + lastOut
}
