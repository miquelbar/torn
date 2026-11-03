package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

// FormatExecArgs ensures interactive session with initial prompt via -i flag.
func (a *AgyProvider) FormatExecArgs(prompt string) []string {
	if prompt == "" {
		return []string{"agy"}
	}
	return []string{"agy", "-i", prompt}
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
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		var item struct {
			ConversationID string `json:"conversationId"`
			SessionID      string `json:"session_id"`
			Workspace      string `json:"workspace"`
			Cwd            string `json:"cwd"`
			Project        string `json:"project"`
			Timestamp      int64  `json:"timestamp"`
		}

		if err := json.Unmarshal(scanner.Bytes(), &item); err == nil {
			convID := item.ConversationID
			if convID == "" {
				convID = item.SessionID
			}

			ws := item.Workspace
			if ws == "" {
				ws = item.Cwd
			}
			if ws == "" {
				ws = item.Project
			}

			if filepath.Clean(ws) == cleanTarget && convID != "" {
				updated := time.Unix(item.Timestamp/1000, 0)
				if item.Timestamp < 10000000000 {
					updated = time.Unix(item.Timestamp, 0)
				}

				// Locate transcript
				transcriptPath := filepath.Join(a.baseDir, "brain", convID, ".system_generated", "logs", "transcript.jsonl")
				if _, err := os.Stat(transcriptPath); err != nil {
					// Fallback to conversation folder
					transcriptPath = filepath.Join(a.baseDir, "conversations", convID)
				}

				lastMatch = &Session{
					ID:        convID,
					Provider:  a.Name(),
					Path:      transcriptPath,
					CWD:       ws,
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

	convID := filepath.Base(sessionPath)
	if strings.HasSuffix(convID, ".jsonl") {
		// Parent path or extract UUID
		parts := strings.Split(sessionPath, string(filepath.Separator))
		for i, part := range parts {
			if part == "brain" && i+1 < len(parts) {
				convID = parts[i+1]
				break
			}
		}
	}

	details := &SessionDetails{
		Session: Session{
			ID:        convID,
			Provider:  a.Name(),
			Path:      sessionPath,
			UpdatedAt: stat.ModTime(),
		},
		Turns: make([]Turn, 0),
	}

	f, err := os.Open(sessionPath)
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

		var step struct {
			Type         string `json:"type"`
			Source       string `json:"source"`
			Content      string `json:"content"`
			Thinking     string `json:"thinking"`
			InputTokens  int    `json:"input_tokens"`
			OutputTokens int    `json:"output_tokens"`
			ToolCalls    []struct {
				Name string         `json:"name"`
				Args map[string]any `json:"args"`
			} `json:"tool_calls"`
		}

		if err := json.Unmarshal(line, &step); err != nil {
			continue
		}

		turnTokens := step.InputTokens + step.OutputTokens
		details.TotalTokens += int64(turnTokens)

		if step.Type == "USER_INPUT" && step.Content != "" {
			userText := cleanUserPrompt(step.Content)
			details.Turns = append(details.Turns, Turn{
				Role:      "user",
				Text:      userText,
				Timestamp: stat.ModTime(),
				Tokens:    turnTokens,
			})
			details.LastUserMessage = userText
		} else if step.Type == "PLANNER_RESPONSE" {
			var tools []string
			for _, tc := range step.ToolCalls {
				toolStr := tc.Name
				argSummary := extractArgSummary(tc.Args)
				cleanArg := strings.Trim(strings.TrimSpace(argSummary), `"`)
				if cleanArg != "" {
					toolStr = fmt.Sprintf("%s(%q)", tc.Name, cleanArg)
				}
				tools = append(tools, toolStr)
			}

			text := cleanThinking(step.Thinking)
			if text != "" || len(tools) > 0 {
				details.Turns = append(details.Turns, Turn{
					Role:      "assistant",
					Text:      text,
					Tools:     tools,
					Tokens:    turnTokens,
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

func (a *AgyProvider) CollectUsage(now time.Time) (UsageMetrics, error) {
	var metrics UsageMetrics
	historyPath := filepath.Join(a.baseDir, "history.jsonl")

	f, err := os.Open(historyPath)
	if err != nil {
		return metrics, nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfWeek := startOfDay.AddDate(0, 0, -7)

	seenConvs := make(map[string]bool)
	for scanner.Scan() {
		var item struct {
			ConversationID string `json:"conversationId"`
			Timestamp      int64  `json:"timestamp"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &item); err == nil && item.ConversationID != "" {
			if seenConvs[item.ConversationID] {
				continue
			}
			seenConvs[item.ConversationID] = true
			metrics.TotalSessions++

			updated := time.Unix(item.Timestamp/1000, 0)
			if item.Timestamp < 10000000000 {
				updated = time.Unix(item.Timestamp, 0)
			}

			transcriptPath := filepath.Join(a.baseDir, "brain", item.ConversationID, ".system_generated", "logs", "transcript.jsonl")
			tokens := a.scanTranscriptTokens(transcriptPath)
			metrics.TotalTokens += tokens

			if updated.After(startOfWeek) {
				metrics.TokensThisWeek += tokens
			}
			if updated.After(startOfDay) {
				metrics.TokensToday += tokens
			}
		}
	}

	return metrics, nil
}

func (a *AgyProvider) scanTranscriptTokens(path string) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()

	var total int64
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if !strings.Contains(string(line), `"input_tokens"`) {
			continue
		}

		var step struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		}
		if err := json.Unmarshal(line, &step); err == nil {
			total += int64(step.InputTokens + step.OutputTokens)
		}
	}

	return total
}

var userRequestRegex = regexp.MustCompile(`(?s)<\/?USER_REQUEST>`)
var truncatedMarkerRegex = regexp.MustCompile(`(?i)<truncated\s+[^>]+>`)
var additionalMetaRegex = regexp.MustCompile(`(?s)<ADDITIONAL_METADATA>.*?(?:</ADDITIONAL_METADATA>|$)`)

func cleanUserPrompt(s string) string {
	res := userRequestRegex.ReplaceAllString(s, "")
	res = additionalMetaRegex.ReplaceAllString(res, "")
	res = truncatedMarkerRegex.ReplaceAllString(res, "")
	return strings.TrimSpace(res)
}

func cleanThinking(s string) string {
	res := truncatedMarkerRegex.ReplaceAllString(s, "")
	return strings.TrimSpace(res)
}
