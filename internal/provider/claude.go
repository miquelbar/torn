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
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	seenMsgIDs := make(map[string]bool)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}

		if strings.Contains(string(line), "resets ") {
			if resetTime := ExtractResetTime(string(line), stat.ModTime(), time.Now()); resetTime != nil {
				details.CooldownUntil = resetTime
			}
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

			var turnTokens int
			if msgMap != nil {
				if usage, ok := msgMap["usage"].(map[string]any); ok {
					inTok := toInt64(usage["input_tokens"]) + toInt64(usage["cache_creation_input_tokens"])
					outTok := toInt64(usage["output_tokens"])
					turnTokens = int(inTok + outTok)
					msgID, _ := msgMap["id"].(string)
					if msgID == "" || !seenMsgIDs[msgID] {
						if msgID != "" {
							seenMsgIDs[msgID] = true
						}
						details.TotalTokens += inTok + outTok
					}
				}
			}

			modelStr, _ := msgMap["model"].(string)
			if modelStr == "" {
				modelStr, _ = raw["model"].(string)
			}
			normModel := FormatModelName(modelStr)
			if normModel != "" {
				found := false
				for _, m := range details.ModelsUsed {
					if m == normModel {
						found = true
						break
					}
				}
				if !found {
					details.ModelsUsed = append(details.ModelsUsed, normModel)
				}
			}

			if extracted.Text != "" || len(extracted.Tools) > 0 {
				details.Turns = append(details.Turns, Turn{
					Role:      "assistant",
					Text:      extracted.Text,
					Tools:     extracted.Tools,
					Tokens:    turnTokens,
					Model:     normModel,
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

func (c *ClaudeProvider) CollectUsage(now time.Time, projectDir ...string) (UsageMetrics, error) {
	metrics := UsageMetrics{
		ModelStats: make(map[string]ModelMetric),
	}
	projectsDir := filepath.Join(c.baseDir, "projects")

	var targetSlug string
	if len(projectDir) > 0 && projectDir[0] != "" {
		targetSlug = c.PathToSlug(projectDir[0])
	}

	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfWeek := startOfDay.AddDate(0, 0, -7)

	var totalValidOutTok int64
	var totalValidDuration float64
	var latestModelTime time.Time
	uniqueModelsMap := make(map[string]bool)

	processDir := func(pDir string) {
		files, err := os.ReadDir(pDir)
		if err != nil {
			return
		}

		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			metrics.TotalSessions++
			fInfo, err := f.Info()
			if err != nil {
				continue
			}

			filePath := filepath.Join(pDir, f.Name())
			res := c.scanFileTokens(filePath, fInfo.ModTime(), startOfDay, startOfWeek)
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

	if targetSlug != "" {
		pDir := filepath.Join(projectsDir, targetSlug)
		if info, err := os.Stat(pDir); err == nil && info.IsDir() {
			processDir(pDir)
		}
	} else {
		entries, err := os.ReadDir(projectsDir)
		if err == nil {
			for _, pEntry := range entries {
				if pEntry.IsDir() {
					processDir(filepath.Join(projectsDir, pEntry.Name()))
				}
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

type claudeFileStats struct {
	totalTokens    int64
	tokensThisWeek int64
	tokensToday    int64
	validOutputTok int64
	validDuration  float64
	modelStats     map[string]ModelMetric
	lastModel      string
	lastModelTime  time.Time
}

func (c *ClaudeProvider) scanFileTokens(path string, fileMod time.Time, startOfDay, startOfWeek time.Time) claudeFileStats {
	res := claudeFileStats{
		modelStats: make(map[string]ModelMetric),
	}

	f, err := os.Open(path)
	if err != nil {
		return res
	}
	defer f.Close()

	type msgRecord struct {
		id           string
		model        string
		tokens       int64
		outputTokens int64
		time         time.Time
		durationSecs float64
	}
	seenMsgs := make(map[string]msgRecord)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)

	var prevEventTime time.Time

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var base struct {
			Timestamp string `json:"timestamp"`
		}
		_ = json.Unmarshal(line, &base)
		var curTime time.Time
		if base.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, base.Timestamp); err == nil {
				curTime = t
			} else if t, err := time.Parse(time.RFC3339, base.Timestamp); err == nil {
				curTime = t
			}
		}

		if !strings.Contains(string(line), `"usage"`) {
			if !curTime.IsZero() {
				prevEventTime = curTime
			}
			continue
		}

		var raw struct {
			Timestamp string `json:"timestamp"`
			Message   struct {
				ID    string         `json:"id"`
				Model string         `json:"model"`
				Usage map[string]any `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &raw); err == nil && raw.Message.Usage != nil {
			u := raw.Message.Usage
			inTok := toInt64(u["input_tokens"]) + toInt64(u["cache_creation_input_tokens"])
			outTok := toInt64(u["output_tokens"])
			msgTokens := inTok + outTok

			msgTime := fileMod
			if !curTime.IsZero() {
				msgTime = curTime
			} else if raw.Timestamp != "" {
				if t, err := time.Parse(time.RFC3339Nano, raw.Timestamp); err == nil {
					msgTime = t
				} else if t, err := time.Parse(time.RFC3339, raw.Timestamp); err == nil {
					msgTime = t
				}
			}

			model := FormatModelName(raw.Message.Model)
			if model == "" {
				model = "claude-sonnet-4-5"
			}

			var dt float64
			if !prevEventTime.IsZero() && !msgTime.IsZero() {
				diff := msgTime.Sub(prevEventTime).Seconds()
				if outTok >= 30 && diff >= 0.5 && diff <= 45.0 {
					dt = diff
				}
			}

			id := raw.Message.ID
			if id == "" {
				id = fmt.Sprintf("anon_%d", len(seenMsgs))
			}
			seenMsgs[id] = msgRecord{
				id:           id,
				model:        model,
				tokens:       msgTokens,
				outputTokens: outTok,
				time:         msgTime,
				durationSecs: dt,
			}

			if !msgTime.IsZero() {
				prevEventTime = msgTime
			}
		}
	}

	for _, rec := range seenMsgs {
		res.totalTokens += rec.tokens
		isToday := rec.time.After(startOfDay) || rec.time.Equal(startOfDay)
		isWeek := rec.time.After(startOfWeek) || rec.time.Equal(startOfWeek)
		if isWeek {
			res.tokensThisWeek += rec.tokens
		}
		if isToday {
			res.tokensToday += rec.tokens
		}

		m := res.modelStats[rec.model]
		m.Model = rec.model
		m.Turns++
		m.TotalTokens += rec.tokens
		if isWeek {
			m.TokensThisWeek += rec.tokens
		}
		if isToday {
			m.TokensToday += rec.tokens
		}
		if rec.durationSecs > 0 {
			m.OutputTokens += rec.outputTokens
			m.DurationSecs += rec.durationSecs
			res.validOutputTok += rec.outputTokens
			res.validDuration += rec.durationSecs
		}
		res.modelStats[rec.model] = m

		if rec.time.After(res.lastModelTime) {
			res.lastModel = rec.model
			res.lastModelTime = rec.time
		}
	}

	return res
}

func toInt64(v any) int64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}
