package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// OpenCodeProvider manages OpenCode CLI sessions and telemetry from SQLite.
type OpenCodeProvider struct {
	baseDir   string
	sqliteBin string
}

// NewOpenCode creates an OpenCodeProvider.
// If baseDir is empty, defaults to ~/.local/share/opencode.
func NewOpenCode(baseDir string) *OpenCodeProvider {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".local", "share", "opencode")
	}
	return &OpenCodeProvider{
		baseDir:   baseDir,
		sqliteBin: "sqlite3",
	}
}

func (o *OpenCodeProvider) Name() string       { return "opencode" }
func (o *OpenCodeProvider) BinaryName() string { return "opencode" }

func (o *OpenCodeProvider) DefaultCooldown() time.Duration {
	return 4 * time.Hour
}

// FormatExecArgs returns the command-line arguments to run OpenCode.
// Empty prompt launches interactive TUI mode; non-empty passes prompt via interactive run flag.
func (o *OpenCodeProvider) FormatExecArgs(prompt string) []string {
	if prompt == "" {
		return []string{"opencode"}
	}
	return []string{"opencode", "run", "-i", prompt}
}

func (o *OpenCodeProvider) dbPath() string {
	return filepath.Join(o.baseDir, "opencode.db")
}

func (o *OpenCodeProvider) queryJSON(ctx context.Context, query string, target any) error {
	return o.queryJSONWithDB(ctx, o.dbPath(), query, target)
}

func (o *OpenCodeProvider) queryJSONWithDB(ctx context.Context, db string, query string, target any) error {
	if _, err := os.Stat(db); err != nil {
		return err
	}
	if _, err := exec.LookPath(o.sqliteBin); err != nil {
		return fmt.Errorf("sqlite3 binary not found in PATH: %w", err)
	}

	cmd := exec.CommandContext(ctx, o.sqliteBin, "-readonly", "-json", db, query)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sqlite query failed: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	out := stdout.Bytes()
	if len(bytes.TrimSpace(out)) == 0 {
		return nil
	}

	return json.Unmarshal(out, target)
}

// DetectLatestSession finds the newest session matching targetCwd in the SQLite database.
func (o *OpenCodeProvider) DetectLatestSession(targetCwd string) (*Session, error) {
	cleanTarget := filepath.Clean(targetCwd)
	escapedCwd := strings.ReplaceAll(cleanTarget, "'", "''")

	query := fmt.Sprintf(
		"SELECT id, directory, time_updated FROM session WHERE directory = '%s' ORDER BY time_updated DESC LIMIT 1;",
		escapedCwd,
	)

	type row struct {
		ID          string `json:"id"`
		Directory   string `json:"directory"`
		TimeUpdated int64  `json:"time_updated"`
	}
	var rows []row
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := o.queryJSON(ctx, query, &rows); err != nil {
		return nil, fmt.Errorf("detecting opencode session: %w", err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("no opencode sessions found for %q", targetCwd)
	}

	r := rows[0]
	return &Session{
		ID:        r.ID,
		Provider:  o.Name(),
		Path:      fmt.Sprintf("%s#%s", o.dbPath(), r.ID),
		CWD:       r.Directory,
		UpdatedAt: time.UnixMilli(r.TimeUpdated),
	}, nil
}

// ParseSession extracts conversation turns, tool calls, and model metadata from SQLite.
func (o *OpenCodeProvider) ParseSession(sessionPath string) (*SessionDetails, error) {
	db := o.dbPath()
	sessionID := sessionPath

	if idx := strings.Index(sessionPath, "#"); idx != -1 {
		db = sessionPath[:idx]
		sessionID = sessionPath[idx+1:]
	} else if strings.HasSuffix(sessionPath, ".db") {
		db = sessionPath
		sessionID = ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if sessionID == "" {
		var latest []struct {
			ID string `json:"id"`
		}
		if err := o.queryJSONWithDB(ctx, db, "SELECT id FROM session ORDER BY time_updated DESC LIMIT 1;", &latest); err != nil {
			return nil, err
		}
		if len(latest) == 0 {
			return nil, fmt.Errorf("no sessions found in opencode database %q", db)
		}
		sessionID = latest[0].ID
	}

	escapedID := strings.ReplaceAll(sessionID, "'", "''")

	type sessRow struct {
		ID              string `json:"id"`
		Directory       string `json:"directory"`
		TimeUpdated     int64  `json:"time_updated"`
		Model           string `json:"model"`
		TokensInput     int64  `json:"tokens_input"`
		TokensOutput    int64  `json:"tokens_output"`
		TokensReasoning int64  `json:"tokens_reasoning"`
	}
	var sRows []sessRow
	qSess := fmt.Sprintf("SELECT id, directory, time_updated, model, tokens_input, tokens_output, tokens_reasoning FROM session WHERE id = '%s' LIMIT 1;", escapedID)
	if err := o.queryJSONWithDB(ctx, db, qSess, &sRows); err != nil {
		return nil, fmt.Errorf("reading opencode session %q: %w", sessionID, err)
	}
	if len(sRows) == 0 {
		return nil, fmt.Errorf("opencode session %q not found", sessionID)
	}
	s := sRows[0]

	details := &SessionDetails{
		Session: Session{
			ID:        s.ID,
			Provider:  o.Name(),
			Path:      fmt.Sprintf("%s#%s", db, s.ID),
			CWD:       s.Directory,
			UpdatedAt: time.UnixMilli(s.TimeUpdated),
		},
		Turns: make([]Turn, 0),
	}

	// Fetch parts grouped by message_id
	type partRow struct {
		MessageID string `json:"message_id"`
		Data      string `json:"data"`
	}
	var pRows []partRow
	qPart := fmt.Sprintf("SELECT message_id, data FROM part WHERE session_id = '%s' ORDER BY time_created ASC;", escapedID)
	_ = o.queryJSONWithDB(ctx, db, qPart, &pRows)

	type parsedPart struct {
		text  string
		tools []string
		files []string
	}
	partsByMsg := make(map[string]*parsedPart)

	for _, pr := range pRows {
		var pd struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Tool  string `json:"tool"`
			State struct {
				Input map[string]any `json:"input"`
				Title string         `json:"title"`
			} `json:"state"`
		}
		if err := json.Unmarshal([]byte(pr.Data), &pd); err != nil {
			continue
		}

		entry := partsByMsg[pr.MessageID]
		if entry == nil {
			entry = &parsedPart{}
			partsByMsg[pr.MessageID] = entry
		}

		switch pd.Type {
		case "text", "reasoning":
			if pd.Text != "" {
				if entry.text != "" {
					entry.text += "\n"
				}
				entry.text += pd.Text
			}
		case "tool":
			toolName := pd.Tool
			if pd.State.Title != "" {
				toolName = fmt.Sprintf("%s(%q)", pd.Tool, pd.State.Title)
			} else if cmd, ok := pd.State.Input["command"].(string); ok && cmd != "" {
				toolName = fmt.Sprintf("%s(%q)", pd.Tool, cmd)
			}
			entry.tools = append(entry.tools, toolName)

			for _, key := range []string{"path", "filePath", "file"} {
				if fPath, ok := pd.State.Input[key].(string); ok && fPath != "" {
					entry.files = append(entry.files, fPath)
				}
			}
		}
	}

	// Fetch messages
	type msgRow struct {
		ID          string `json:"id"`
		TimeCreated int64  `json:"time_created"`
		Data        string `json:"data"`
	}
	var mRows []msgRow
	qMsg := fmt.Sprintf("SELECT id, time_created, data FROM message WHERE session_id = '%s' ORDER BY time_created ASC;", escapedID)
	_ = o.queryJSONWithDB(ctx, db, qMsg, &mRows)

	for _, mr := range mRows {
		var md struct {
			Role    string `json:"role"`
			ModelID string `json:"modelID"`
			Tokens  struct {
				Input     int `json:"input"`
				Output    int `json:"output"`
				Reasoning int `json:"reasoning"`
			} `json:"tokens"`
		}
		if err := json.Unmarshal([]byte(mr.Data), &md); err != nil {
			continue
		}

		turnModel := parseModelIdentifier(md.ModelID)
		if turnModel != "" {
			found := false
			for _, m := range details.ModelsUsed {
				if m == turnModel {
					found = true
					break
				}
			}
			if !found {
				details.ModelsUsed = append(details.ModelsUsed, turnModel)
			}
		}

		partEntry := partsByMsg[mr.ID]
		turnText := ""
		var turnTools []string
		var turnFiles []string
		if partEntry != nil {
			turnText = partEntry.text
			turnTools = partEntry.tools
			turnFiles = partEntry.files
		}

		msgTokens := md.Tokens.Input + md.Tokens.Output + md.Tokens.Reasoning
		details.TotalTokens += int64(msgTokens)

		t := time.UnixMilli(mr.TimeCreated)
		if md.Role == "user" {
			turnText = cleanUserPrompt(turnText)
			if turnText != "" {
				details.LastUserMessage = turnText
			}
			details.Turns = append(details.Turns, Turn{
				Role:      "user",
				Text:      turnText,
				Timestamp: t,
				Tokens:    msgTokens,
				Model:     turnModel,
			})
		} else if md.Role == "assistant" {
			if turnText != "" {
				details.LastAssistantMessage = turnText
			}
			details.Turns = append(details.Turns, Turn{
				Role:         "assistant",
				Text:         turnText,
				Tools:        turnTools,
				FilesTouched: turnFiles,
				Timestamp:    t,
				Tokens:       msgTokens,
				Model:        turnModel,
			})
		}
	}

	if details.TotalTokens == 0 {
		details.TotalTokens = s.TokensInput + s.TokensOutput + s.TokensReasoning
	}
	if len(details.ModelsUsed) == 0 && s.Model != "" {
		if m := parseModelIdentifier(s.Model); m != "" {
			details.ModelsUsed = append(details.ModelsUsed, m)
		}
	}

	return details, nil
}

// CollectUsage computes session counts, token burn, speeds, and model breakdowns from SQLite.
func (o *OpenCodeProvider) CollectUsage(now time.Time, projectDir ...string) (UsageMetrics, error) {
	metrics := UsageMetrics{
		ModelStats: make(map[string]ModelMetric),
	}

	db := o.dbPath()
	if _, err := os.Stat(db); err != nil {
		return metrics, nil
	}

	var cleanTarget string
	if len(projectDir) > 0 && projectDir[0] != "" {
		cleanTarget = filepath.Clean(projectDir[0])
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	whereSession := ""
	whereMsgJoin := ""
	if cleanTarget != "" {
		escapedTarget := strings.ReplaceAll(cleanTarget, "'", "''")
		whereSession = fmt.Sprintf("WHERE directory = '%s'", escapedTarget)
		whereMsgJoin = fmt.Sprintf("JOIN session s ON m.session_id = s.id WHERE s.directory = '%s' AND", escapedTarget)
	} else {
		whereMsgJoin = "WHERE"
	}

	type sessSummary struct {
		Count int   `json:"cnt"`
		Total int64 `json:"tot"`
	}
	var sessSum []sessSummary
	qSess := fmt.Sprintf(
		"SELECT count(*) as cnt, coalesce(sum(tokens_input + tokens_output + tokens_reasoning), 0) as tot FROM session %s;",
		whereSession,
	)
	if err := o.queryJSONWithDB(ctx, db, qSess, &sessSum); err == nil && len(sessSum) > 0 {
		metrics.TotalSessions = sessSum[0].Count
		metrics.TotalTokens = sessSum[0].Total
	}

	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfWeek := startOfDay.AddDate(0, 0, -7)
	startOfDayMs := startOfDay.UnixMilli()
	startOfWeekMs := startOfWeek.UnixMilli()

	type modelAgg struct {
		RawModel     string  `json:"raw_model"`
		Turns        int     `json:"turns"`
		TotalTokens  int64   `json:"total_tokens"`
		TodayTokens  int64   `json:"today_tokens"`
		WeekTokens   int64   `json:"week_tokens"`
		OutTokens    int64   `json:"out_tokens"`
		DurationSecs float64 `json:"duration_secs"`
	}
	var aggs []modelAgg
	qAgg := fmt.Sprintf(`
SELECT 
    coalesce(json_extract(m.data, '$.modelID'), '') as raw_model,
    count(*) as turns,
    sum(coalesce(json_extract(m.data, '$.tokens.input'), 0) + coalesce(json_extract(m.data, '$.tokens.output'), 0) + coalesce(json_extract(m.data, '$.tokens.reasoning'), 0)) as total_tokens,
    sum(CASE WHEN m.time_created >= %d THEN coalesce(json_extract(m.data, '$.tokens.input'), 0) + coalesce(json_extract(m.data, '$.tokens.output'), 0) + coalesce(json_extract(m.data, '$.tokens.reasoning'), 0) ELSE 0 END) as today_tokens,
    sum(CASE WHEN m.time_created >= %d THEN coalesce(json_extract(m.data, '$.tokens.input'), 0) + coalesce(json_extract(m.data, '$.tokens.output'), 0) + coalesce(json_extract(m.data, '$.tokens.reasoning'), 0) ELSE 0 END) as week_tokens,
    sum(coalesce(json_extract(m.data, '$.tokens.output'), 0) + coalesce(json_extract(m.data, '$.tokens.reasoning'), 0)) as out_tokens,
    sum(CASE WHEN json_extract(m.data, '$.time.completed') > json_extract(m.data, '$.time.created') THEN (json_extract(m.data, '$.time.completed') - json_extract(m.data, '$.time.created')) / 1000.0 ELSE 0 END) as duration_secs
FROM message m
%s json_extract(m.data, '$.role') = 'assistant'
GROUP BY raw_model;`,
		startOfDayMs, startOfWeekMs, whereMsgJoin,
	)

	var totalValidOutTok int64
	var totalValidDuration float64
	uniqueModelsMap := make(map[string]bool)

	if err := o.queryJSONWithDB(ctx, db, qAgg, &aggs); err == nil {
		for _, agg := range aggs {
			model := parseModelIdentifier(agg.RawModel)
			if model == "" {
				model = "opencode"
			}
			uniqueModelsMap[model] = true

			metrics.TokensToday += agg.TodayTokens
			metrics.TokensThisWeek += agg.WeekTokens
			totalValidOutTok += agg.OutTokens
			totalValidDuration += agg.DurationSecs

			cur := metrics.ModelStats[model]
			cur.Model = model
			cur.Turns += agg.Turns
			cur.TokensToday += agg.TodayTokens
			cur.TokensThisWeek += agg.WeekTokens
			cur.TotalTokens += agg.TotalTokens
			cur.OutputTokens += agg.OutTokens
			cur.DurationSecs += agg.DurationSecs
			metrics.ModelStats[model] = cur
		}
	}

	for model, ms := range metrics.ModelStats {
		if ms.DurationSecs > 0 && ms.OutputTokens > 0 {
			ms.TokensPerSec = float64(ms.OutputTokens) / ms.DurationSecs
			metrics.ModelStats[model] = ms
		}
	}

	if totalValidDuration > 0 && totalValidOutTok > 0 {
		metrics.TokensPerSec = float64(totalValidOutTok) / totalValidDuration
	}

	for m := range uniqueModelsMap {
		metrics.UniqueModels = append(metrics.UniqueModels, m)
	}
	sort.Strings(metrics.UniqueModels)

	type lastRow struct {
		Model string `json:"model"`
	}
	var lastRows []lastRow
	qLast := fmt.Sprintf(`
SELECT json_extract(m.data, '$.modelID') as model
FROM message m
%s json_extract(m.data, '$.role') = 'assistant' AND json_extract(m.data, '$.modelID') IS NOT NULL
ORDER BY m.time_created DESC LIMIT 1;`,
		whereMsgJoin,
	)
	if err := o.queryJSONWithDB(ctx, db, qLast, &lastRows); err == nil && len(lastRows) > 0 {
		metrics.LastModel = parseModelIdentifier(lastRows[0].Model)
	}

	if metrics.LastModel == "" && metrics.TotalSessions > 0 {
		var lastSess []struct {
			Model string `json:"model"`
		}
		qSessLast := fmt.Sprintf("SELECT model FROM session %s ORDER BY time_updated DESC LIMIT 1;", whereSession)
		if err := o.queryJSONWithDB(ctx, db, qSessLast, &lastSess); err == nil && len(lastSess) > 0 {
			metrics.LastModel = parseModelIdentifier(lastSess[0].Model)
		}
	}

	return metrics, nil
}

func parseModelIdentifier(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "{") {
		var obj struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(raw), &obj); err == nil && obj.ID != "" {
			raw = obj.ID
		}
	}
	if idx := strings.Index(raw, "/"); idx != -1 {
		raw = raw[idx+1:]
	}
	return FormatModelName(raw)
}
