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

	activeModel := "glm-5-3"
	metaPath := filepath.Join(sessionPath, "meta.json")
	if !stat.IsDir() {
		metaPath = filepath.Join(filepath.Dir(sessionPath), "meta.json")
	}
	if metaData, err := os.ReadFile(metaPath); err == nil {
		var meta struct {
			Config struct {
				ActiveModel string `json:"active_model"`
			} `json:"config"`
			Experiments struct {
				DefaultValue struct {
					ActiveModel string `json:"active_model"`
				} `json:"defaultValue"`
			} `json:"experiments"`
		}
		if err := json.Unmarshal(metaData, &meta); err == nil {
			if m := FormatModelName(meta.Config.ActiveModel); m != "" {
				activeModel = m
			} else if m := FormatModelName(meta.Experiments.DefaultValue.ActiveModel); m != "" {
				activeModel = m
			}
		}
	}
	if activeModel != "" {
		details.ModelsUsed = append(details.ModelsUsed, activeModel)
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
				Model:     activeModel,
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

func (v *VibeProvider) CollectUsage(now time.Time, projectDir ...string) (UsageMetrics, error) {
	metrics := UsageMetrics{
		ModelStats: make(map[string]ModelMetric),
	}

	var cleanTarget string
	if len(projectDir) > 0 && projectDir[0] != "" {
		cleanTarget = filepath.Clean(projectDir[0])
	}

	allowedSessions := make(map[string]bool)
	if cleanTarget != "" {
		indexPath := filepath.Join(v.baseDir, "logs", "session", ".session_index.json")
		if data, err := os.ReadFile(indexPath); err == nil {
			var index map[string]vibeIndexEntry
			if err := json.Unmarshal(data, &index); err == nil {
				for key, entry := range index {
					if filepath.Clean(entry.CWD) == cleanTarget {
						allowedSessions[key] = true
					}
				}
			}
		}
	}

	sessionDir := filepath.Join(v.baseDir, "logs", "session")
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return metrics, nil
	}

	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfWeek := startOfDay.AddDate(0, 0, -7)

	var totalValidOutTok int64
	var totalValidDuration float64
	var latestModelTime time.Time
	uniqueModelsMap := make(map[string]bool)

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if cleanTarget != "" && !allowedSessions[entry.Name()] {
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
				PromptTokens     int64   `json:"session_prompt_tokens"`
				CachedTokens     int64   `json:"session_cached_tokens"`
				CompletionTokens int64   `json:"session_completion_tokens"`
				TotalTokens      int64   `json:"session_total_llm_tokens"`
				TokensPerSecond  float64 `json:"tokens_per_second"`
				LastTurnDuration float64 `json:"last_turn_duration"`
				Steps            int     `json:"steps"`
			} `json:"stats"`
			Config struct {
				ActiveModel string `json:"active_model"`
			} `json:"config"`
			Experiments struct {
				DefaultValue struct {
					ActiveModel string `json:"active_model"`
				} `json:"defaultValue"`
			} `json:"experiments"`
		}
		if err := json.Unmarshal(metaData, &meta); err == nil {
			model := FormatModelName(meta.Config.ActiveModel)
			if model == "" {
				model = FormatModelName(meta.Experiments.DefaultValue.ActiveModel)
			}
			if model == "" {
				model = "glm-5-3"
			}
			uniqueModelsMap[model] = true

			nonCachedPrompt := meta.Stats.PromptTokens - meta.Stats.CachedTokens
			if nonCachedPrompt < 0 {
				nonCachedPrompt = 0
			}
			tokens := nonCachedPrompt + meta.Stats.CompletionTokens
			if tokens == 0 && meta.Stats.TotalTokens > 0 {
				tokens = meta.Stats.TotalTokens
			}
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
			// If messages.jsonl or session folder was modified more recently, use latest activity time
			msgPath := filepath.Join(sessionDir, entry.Name(), "messages.jsonl")
			if s, err := os.Stat(msgPath); err == nil && s.ModTime().After(t) {
				t = s.ModTime()
			} else if info, err := entry.Info(); err == nil && info.ModTime().After(t) {
				t = info.ModTime()
			}

			isWeek := t.After(startOfWeek) || t.Equal(startOfWeek)
			isToday := t.After(startOfDay) || t.Equal(startOfDay)

			if isWeek {
				metrics.TokensThisWeek += tokens
			}
			if isToday {
				metrics.TokensToday += tokens
			}

			m := metrics.ModelStats[model]
			m.Model = model
			m.Turns += meta.Stats.Steps
			m.TotalTokens += tokens
			if isWeek {
				m.TokensThisWeek += tokens
			}
			if isToday {
				m.TokensToday += tokens
			}

			if meta.Stats.TokensPerSecond > 0 && meta.Stats.CompletionTokens > 0 {
				duration := float64(meta.Stats.CompletionTokens) / meta.Stats.TokensPerSecond
				m.OutputTokens += meta.Stats.CompletionTokens
				m.DurationSecs += duration
				totalValidOutTok += meta.Stats.CompletionTokens
				totalValidDuration += duration
			}
			metrics.ModelStats[model] = m

			if t.After(latestModelTime) {
				metrics.LastModel = model
				latestModelTime = t
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
