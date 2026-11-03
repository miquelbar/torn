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

	if details.Session.CWD == "" {
		if ws := a.lookupConversationWorkspace(convID); ws != "" {
			details.Session.CWD = ws
		} else {
			details.Session.CWD, _ = os.Getwd()
		}
	}

	f, err := os.Open(sessionPath)
	if err != nil {
		return details, nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	activeModel := "gemini-3.8-flash"

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if match := agyModelChangeRegex.FindSubmatch(line); len(match) > 1 {
			if norm := FormatModelName(string(match[1])); norm != "" {
				activeModel = norm
			}
		}

		recordModel := func() {
			if activeModel == "" {
				return
			}
			for _, m := range details.ModelsUsed {
				if m == activeModel {
					return
				}
			}
			details.ModelsUsed = append(details.ModelsUsed, activeModel)
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
			recordModel()
			userText := cleanUserPrompt(step.Content)
			details.Turns = append(details.Turns, Turn{
				Role:      "user",
				Text:      userText,
				Timestamp: stat.ModTime(),
				Tokens:    turnTokens,
				Model:     activeModel,
			})
			details.LastUserMessage = userText
		} else if step.Type == "PLANNER_RESPONSE" {
			recordModel()
			var tools []string
			for _, tc := range step.ToolCalls {
				toolStr := tc.Name
				argSummary := extractArgSummary(tc.Args, details.Session.CWD)
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
					Model:     activeModel,
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
	metrics := UsageMetrics{
		ModelStats: make(map[string]ModelMetric),
	}
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

	var totalValidOutTok int64
	var totalValidDuration float64
	var latestModelTime time.Time
	uniqueModelsMap := make(map[string]bool)

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

			transcriptPath := filepath.Join(a.baseDir, "brain", item.ConversationID, ".system_generated", "logs", "transcript.jsonl")
			res := a.scanTranscriptTokens(transcriptPath, startOfDay, startOfWeek)
			metrics.TotalTokens += res.totalTokens
			metrics.TokensThisWeek += res.tokensThisWeek
			metrics.TokensToday += res.tokensToday
			totalValidOutTok += res.validOutputTok
			totalValidDuration += res.validDuration

			for model, ms := range res.modelStats {
				uniqueModelsMap[model] = true
				cur := metrics.ModelStats[model]
				cur.Model = model
				cur.Turns += ms.Turns
				cur.TokensToday += ms.TokensToday
				cur.TokensThisWeek += ms.TokensThisWeek
				cur.TotalTokens += ms.TotalTokens
				cur.OutputTokens += ms.OutputTokens
				cur.DurationSecs += ms.DurationSecs
				metrics.ModelStats[model] = cur
			}

			if res.lastModelTime.After(latestModelTime) && res.lastModel != "" {
				metrics.LastModel = res.lastModel
				latestModelTime = res.lastModelTime
			}
		}
	}

	for model, ms := range metrics.ModelStats {
		if ms.DurationSecs > 0 {
			ms.TokensPerSec = float64(ms.OutputTokens) / ms.DurationSecs
			metrics.ModelStats[model] = ms
		}
	}

	if totalValidDuration > 0 {
		metrics.TokensPerSec = float64(totalValidOutTok) / totalValidDuration
	}

	for m := range uniqueModelsMap {
		metrics.UniqueModels = append(metrics.UniqueModels, m)
	}
	sort.Strings(metrics.UniqueModels)

	return metrics, nil
}

type agyFileStats struct {
	totalTokens    int64
	tokensThisWeek int64
	tokensToday    int64
	validOutputTok int64
	validDuration  float64
	modelStats     map[string]ModelMetric
	lastModel      string
	lastModelTime  time.Time
}

func (a *AgyProvider) scanTranscriptTokens(path string, startOfDay, startOfWeek time.Time) agyFileStats {
	res := agyFileStats{
		modelStats: make(map[string]ModelMetric),
	}

	f, err := os.Open(path)
	if err != nil {
		return res
	}
	defer f.Close()

	stat, _ := f.Stat()
	var fileMod time.Time
	if stat != nil {
		fileMod = stat.ModTime()
	}

	activeModel := "gemini-3.8-flash"
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	var prevEventTime time.Time

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		if match := agyModelChangeRegex.FindSubmatch(line); len(match) > 1 {
			if norm := FormatModelName(string(match[1])); norm != "" {
				activeModel = norm
			}
		}

		if !strings.Contains(string(line), `"input_tokens"`) {
			continue
		}

		var step struct {
			CreatedAt    string `json:"created_at"`
			InputTokens  int    `json:"input_tokens"`
			OutputTokens int    `json:"output_tokens"`
		}
		if err := json.Unmarshal(line, &step); err == nil {
			stepTokens := int64(step.InputTokens + step.OutputTokens)
			if stepTokens == 0 {
				continue
			}

			stepTime := fileMod
			if step.CreatedAt != "" {
				if t, err := time.Parse(time.RFC3339Nano, step.CreatedAt); err == nil {
					stepTime = t
				} else if t, err := time.Parse(time.RFC3339, step.CreatedAt); err == nil {
					stepTime = t
				}
			}

			res.totalTokens += stepTokens
			isWeek := !stepTime.IsZero() && (stepTime.After(startOfWeek) || stepTime.Equal(startOfWeek))
			isToday := !stepTime.IsZero() && (stepTime.After(startOfDay) || stepTime.Equal(startOfDay))
			if isWeek {
				res.tokensThisWeek += stepTokens
			}
			if isToday {
				res.tokensToday += stepTokens
			}

			m := res.modelStats[activeModel]
			m.Model = activeModel
			m.Turns++
			m.TotalTokens += stepTokens
			if isWeek {
				m.TokensThisWeek += stepTokens
			}
			if isToday {
				m.TokensToday += stepTokens
			}

			if !prevEventTime.IsZero() && !stepTime.IsZero() {
				dt := stepTime.Sub(prevEventTime).Seconds()
				if step.OutputTokens >= 30 && dt >= 0.5 && dt <= 45.0 {
					outTok := int64(step.OutputTokens)
					m.OutputTokens += outTok
					m.DurationSecs += dt
					res.validOutputTok += outTok
					res.validDuration += dt
				}
			}
			res.modelStats[activeModel] = m

			if stepTime.After(res.lastModelTime) {
				res.lastModel = activeModel
				res.lastModelTime = stepTime
			}

			if !stepTime.IsZero() {
				prevEventTime = stepTime
			}
		}
	}

	return res
}
