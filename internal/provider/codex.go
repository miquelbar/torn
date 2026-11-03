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

	activeModel := "gpt-5.5"

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

		if lineType == "turn_context" || lineType == "session_meta" {
			if m, ok := payload["model"].(string); ok && m != "" {
				if norm := FormatModelName(m); norm != "" {
					activeModel = norm
				}
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

		// 1. Check response_item
		if lineType == "response_item" {
			role, _ := payload["role"].(string)
			extracted := ParseContentBlock(payload["content"])
			if extracted.Text == "" {
				extracted = ParseContentBlock(payload["text"])
			}

			if extracted.Text != "" || len(extracted.Tools) > 0 {
				recordModel()
				details.Turns = append(details.Turns, Turn{
					Role:      role,
					Text:      extracted.Text,
					Tools:     extracted.Tools,
					Model:     activeModel,
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
						recordModel()
						details.Turns = append(details.Turns, Turn{
							Role:      "user",
							Text:      extracted.Text,
							Model:     activeModel,
							Timestamp: stat.ModTime(),
						})
						details.LastUserMessage = extracted.Text
					}
				case "AgentMessage":
					extracted := ParseContentBlock(item["content"])
					if extracted.Text != "" {
						recordModel()
						details.Turns = append(details.Turns, Turn{
							Role:      "assistant",
							Text:      extracted.Text,
							Model:     activeModel,
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
						recordModel()
						details.Turns = append(details.Turns, Turn{
							Role:      "assistant",
							Tools:     []string{fmt.Sprintf("exec(%s)", strings.Join(parts, " "))},
							Model:     activeModel,
							Timestamp: stat.ModTime(),
						})
					}
				}
			}
		}
	}

	return details, scanner.Err()
}

func (c *CodexProvider) CollectUsage(now time.Time, projectDir ...string) (UsageMetrics, error) {
	metrics := UsageMetrics{
		ModelStats: make(map[string]ModelMetric),
	}

	var cleanTarget string
	if len(projectDir) > 0 && projectDir[0] != "" {
		cleanTarget = filepath.Clean(projectDir[0])
	}

	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfWeek := startOfDay.AddDate(0, 0, -7)

	var totalValidOutTok int64
	var totalValidDuration float64
	var latestModelTime time.Time
	uniqueModelsMap := make(map[string]bool)

	sessionsDir := filepath.Join(c.baseDir, "sessions")
	_ = filepath.WalkDir(sessionsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if !strings.HasPrefix(d.Name(), "rollout-") {
			return nil
		}

		if cleanTarget != "" {
			sessionCwd, _ := c.inspectSessionMeta(path)
			if filepath.Clean(sessionCwd) != cleanTarget {
				return nil
			}
		}

		metrics.TotalSessions++
		info, err := d.Info()
		if err != nil {
			return nil
		}

		res := c.scanRolloutTokens(path, info.ModTime(), startOfDay, startOfWeek)
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
		return nil
	})

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

type codexUsageInfo struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
}

type codexFileStats struct {
	totalTokens    int64
	tokensThisWeek int64
	tokensToday    int64
	validOutputTok int64
	validDuration  float64
	modelStats     map[string]ModelMetric
	lastModel      string
	lastModelTime  time.Time
}

func (c *CodexProvider) scanRolloutTokens(path string, fileMod time.Time, startOfDay, startOfWeek time.Time) codexFileStats {
	res := codexFileStats{
		modelStats: make(map[string]ModelMetric),
	}

	f, err := os.Open(path)
	if err != nil {
		return res
	}
	defer f.Close()

	activeModel := "gpt-5.5"
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	var prevEventTime time.Time
	var prevTotalIn, prevTotalCached, prevTotalOut int64

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var raw struct {
			Timestamp string `json:"timestamp"`
			Type      string `json:"type"`
			Payload   struct {
				Model string `json:"model"`
				Info  struct {
					TotalTokenUsage codexUsageInfo `json:"total_token_usage"`
					LastTokenUsage  codexUsageInfo `json:"last_token_usage"`
				} `json:"info"`
			} `json:"payload"`
		}

		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}

		if raw.Type == "turn_context" || raw.Type == "session_meta" {
			if raw.Payload.Model != "" {
				if norm := FormatModelName(raw.Payload.Model); norm != "" {
					activeModel = norm
				}
			}
		}

		var eventTime time.Time
		if raw.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, raw.Timestamp); err == nil {
				eventTime = t
			} else if t, err := time.Parse(time.RFC3339, raw.Timestamp); err == nil {
				eventTime = t
			}
		}
		if eventTime.IsZero() {
			eventTime = fileMod
		}

		lastU := raw.Payload.Info.LastTokenUsage
		totU := raw.Payload.Info.TotalTokenUsage

		var turnIn, turnCached, turnOut int64

		if lastU.InputTokens > 0 || lastU.OutputTokens > 0 {
			turnIn = lastU.InputTokens
			turnCached = lastU.CachedInputTokens
			turnOut = lastU.OutputTokens
		} else if totU.InputTokens > 0 || totU.OutputTokens > 0 {
			if totU.InputTokens >= prevTotalIn && totU.OutputTokens >= prevTotalOut {
				turnIn = totU.InputTokens - prevTotalIn
				turnCached = totU.CachedInputTokens - prevTotalCached
				turnOut = totU.OutputTokens - prevTotalOut
			}
			prevTotalIn = totU.InputTokens
			prevTotalCached = totU.CachedInputTokens
			prevTotalOut = totU.OutputTokens
		}

		nonCachedIn := turnIn - turnCached
		if nonCachedIn < 0 {
			nonCachedIn = 0
		}
		turnTokens := nonCachedIn + turnOut

		if turnTokens > 0 {
			res.totalTokens += turnTokens
			isWeek := eventTime.After(startOfWeek) || eventTime.Equal(startOfWeek)
			isToday := eventTime.After(startOfDay) || eventTime.Equal(startOfDay)
			m := res.modelStats[activeModel]
			m.Model = activeModel
			m.TotalTokens += turnTokens
			m.Turns++
			if isWeek {
				res.tokensThisWeek += turnTokens
				m.TokensThisWeek += turnTokens
			}
			if isToday {
				res.tokensToday += turnTokens
				m.TokensToday += turnTokens
			}

			if !prevEventTime.IsZero() && !eventTime.IsZero() {
				dt := eventTime.Sub(prevEventTime).Seconds()
				if turnOut >= 30 && dt >= 0.5 && dt <= 45.0 {
					m.OutputTokens += turnOut
					m.DurationSecs += dt
					res.validOutputTok += turnOut
					res.validDuration += dt
				}
			}
			res.modelStats[activeModel] = m

			if eventTime.After(res.lastModelTime) {
				res.lastModel = activeModel
				res.lastModelTime = eventTime
			}
		}

		if !eventTime.IsZero() {
			prevEventTime = eventTime
		}
	}

	return res
}
