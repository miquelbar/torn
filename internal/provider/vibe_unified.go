package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Vibe CLI migrated its session store (~Nov 2026, format
// "mistral.vibe.unified-session-store/v1") from
// ~/.vibe/logs/session/session_<ts>_<id>/meta.json (with embedded stats)
// to ~/.vibe/logs/session/unified/<session-id>/ with:
//   - meta.json: session metadata (cwd, model, times), no token stats.
//   - generations/<n>/projection-state.json: latest projected session state
//     holding cumulative tokenUsage over the whole session.
//   - journal/*.jsonl: append-only event log. Projection deltas carry the
//     public session envelope (tokenUsage, latestTurn started/completed times)
//     and appended conversation entries used for handoff parsing.

// vibeUnifiedMeta models meta.json of a unified-store session directory.
type vibeUnifiedMeta struct {
	SessionID   string `json:"session_id"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	BumpedAt    string `json:"bumped_at"`
	ArchivedAt  string `json:"archived_at"`
	Environment struct {
		WorkingDirectory string `json:"working_directory"`
	} `json:"environment"`
	Config struct {
		ActiveModel string `json:"active_model"`
		ModelName   string `json:"model_name"`
	} `json:"config"`
}

// vibeTokenUsage models harness.public-session-state tokenUsage (camelCase).
type vibeTokenUsage struct {
	InputTokens       int64 `json:"inputTokens"`
	CachedInputTokens int64 `json:"cachedInputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	TotalTokens       int64 `json:"totalTokens"`
}

func (u vibeTokenUsage) tokens() int64 {
	nonCached := u.InputTokens - u.CachedInputTokens
	if nonCached < 0 {
		nonCached = 0
	}
	t := nonCached + u.OutputTokens
	if t == 0 {
		t = u.TotalTokens
	}
	return t
}

// vibeUnifiedSession is a discovered unified-store session candidate.
type vibeUnifiedSession struct {
	ID        string
	Dir       string
	CWD       string
	Model     string
	UpdatedAt time.Time
	Meta      vibeUnifiedMeta
}

func (v *VibeProvider) unifiedDir() string {
	return filepath.Join(v.baseDir, "logs", "session", "unified")
}

// parseVibeTime parses the RFC3339-with-microseconds timestamps used by vibe.
func parseVibeTime(raw string) (time.Time, bool) {
	if raw == "" || raw == "null" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// listUnifiedSessions returns unified-store sessions sorted by activity time
// (newest first). If targetCwd is non-empty, only sessions for that project
// directory are returned.
func (v *VibeProvider) listUnifiedSessions(targetCwd string) []vibeUnifiedSession {
	cleanTarget := ""
	if targetCwd != "" {
		cleanTarget = filepath.Clean(targetCwd)
	}

	entries, err := os.ReadDir(v.unifiedDir())
	if err != nil {
		return nil
	}

	sessions := make([]vibeUnifiedSession, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dir := filepath.Join(v.unifiedDir(), entry.Name())

		meta, ok := readVibeUnifiedMeta(dir)
		if !ok {
			continue
		}
		if cleanTarget != "" && filepath.Clean(meta.Environment.WorkingDirectory) != cleanTarget {
			continue
		}

		model := FormatModelName(meta.Config.ActiveModel)
		if model == "" {
			model = FormatModelName(meta.Config.ModelName)
		}
		updatedAt := vibeSessionActivityTime(dir, meta)
		sessions = append(sessions, vibeUnifiedSession{
			ID:        meta.SessionID,
			Dir:       dir,
			CWD:       meta.Environment.WorkingDirectory,
			Model:     model,
			UpdatedAt: updatedAt,
			Meta:      meta,
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions
}

func readVibeUnifiedMeta(sessionDir string) (vibeUnifiedMeta, bool) {
	var meta vibeUnifiedMeta
	data, err := os.ReadFile(filepath.Join(sessionDir, "meta.json"))
	if err != nil {
		return meta, false
	}
	if err := json.Unmarshal(data, &meta); err != nil || meta.SessionID == "" {
		return meta, false
	}
	return meta, true
}

// vibeSessionActivityTime resolves the most recent activity timestamp of a
// unified session: bumped_at, then end_time, then projection-state mtime.
func vibeSessionActivityTime(sessionDir string, meta vibeUnifiedMeta) time.Time {
	for _, raw := range []string{meta.BumpedAt, meta.EndTime} {
		if t, ok := parseVibeTime(raw); ok {
			return t
		}
	}
	if gen, ok := latestVibeGenerationDir(sessionDir); ok {
		if info, err := os.Stat(filepath.Join(gen, "projection-state.json")); err == nil {
			return info.ModTime()
		}
	}
	if info, err := os.Stat(sessionDir); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// latestVibeGenerationDir returns the highest-numbered generation directory.
func latestVibeGenerationDir(sessionDir string) (string, bool) {
	genDir := filepath.Join(sessionDir, "generations")
	entries, err := os.ReadDir(genDir)
	if err != nil {
		return "", false
	}
	best := ""
	var bestNum int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		num, err := strconv.ParseInt(entry.Name(), 10, 64)
		if err != nil {
			continue
		}
		if best == "" || num > bestNum {
			best = entry.Name()
			bestNum = num
		}
	}
	if best == "" {
		return "", false
	}
	return filepath.Join(genDir, best), true
}

// readVibeProjectionUsage reads cumulative token usage from the latest
// generation's projection-state.json.
func readVibeProjectionUsage(sessionDir string) (vibeTokenUsage, bool) {
	gen, ok := latestVibeGenerationDir(sessionDir)
	if !ok {
		return vibeTokenUsage{}, false
	}
	data, err := os.ReadFile(filepath.Join(gen, "projection-state.json"))
	if err != nil {
		return vibeTokenUsage{}, false
	}
	var state struct {
		Snapshot struct {
			Session struct {
				TokenUsage *vibeTokenUsage `json:"tokenUsage"`
			} `json:"session"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return vibeTokenUsage{}, false
	}
	if state.Snapshot.Session.TokenUsage == nil {
		return vibeTokenUsage{}, false
	}
	return *state.Snapshot.Session.TokenUsage, true
}

// vibeJournalStats aggregates per-turn timing and conversation turns from the
// journal segments that have not been folded into generations yet.
type vibeJournalStats struct {
	Turns        []Turn
	TurnCount    int
	OutputTokens int64
	DurationSecs float64
	Usage        vibeTokenUsage // last envelope cumulative usage (fallback)
}

// scanVibeJournal walks non-empty journal segments of a session in order and
// aggregates: conversation entries (for handoff), per-turn output tokens and
// durations (for speed), and the latest cumulative tokenUsage envelope.
func scanVibeJournal(sessionDir string) vibeJournalStats {
	var stats vibeJournalStats

	journalDir := filepath.Join(sessionDir, "journal")
	segments, err := os.ReadDir(journalDir)
	if err != nil {
		return stats
	}
	names := make([]string, 0, len(segments))
	for _, seg := range segments {
		if strings.HasSuffix(seg.Name(), ".jsonl") {
			names = append(names, seg.Name())
		}
	}
	sort.Strings(names)

	// Per-turn tracking via latestTurn transitions in envelopes.
	turnStart := make(map[string]int64)    // turnID -> startedAt ms
	turnOutStart := make(map[string]int64) // turnID -> output tokens at turn start

	for _, name := range names {
		path := filepath.Join(journalDir, name)
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		stats = foldVibeJournalLines(f, stats, turnStart, turnOutStart)
		f.Close()
	}

	return stats
}

func foldVibeJournalLines(f *os.File, stats vibeJournalStats, turnStart, turnOutStart map[string]int64) vibeJournalStats {
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Payload struct {
				Delta []struct {
					Op    string `json:"op"`
					State *struct {
						LatestTurn *struct {
							ID          string `json:"id"`
							StartedAt   int64  `json:"startedAt"`
							CompletedAt int64  `json:"completedAt"`
							Status      string `json:"status"`
						} `json:"latestTurn"`
						Session *struct {
							TokenUsage *vibeTokenUsage `json:"tokenUsage"`
						} `json:"session"`
					} `json:"state"`
					Entry *struct {
						Role      string `json:"role"`
						CreatedAt int64  `json:"createdAt"`
						Content   []struct {
							Text string `json:"text"`
							Type string `json:"type"`
						} `json:"content"`
					} `json:"entry"`
				} `json:"delta"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if ev.Type != "projection_delta" {
			continue
		}
		for _, d := range ev.Payload.Delta {
			switch d.Op {
			case "append_entry":
				stats.Turns = appendVibeEntryTurn(stats.Turns, d.Entry)
			case "set_envelope":
				if d.State == nil {
					continue
				}
				if d.State.Session != nil && d.State.Session.TokenUsage != nil {
					stats.Usage = *d.State.Session.TokenUsage
				}
				stats = trackVibeTurnTiming(stats, d.State.LatestTurn, turnStart, turnOutStart)
			}
		}
	}
	return stats
}

// appendVibeEntryTurn converts a projected conversation entry into a Turn.
func appendVibeEntryTurn(turns []Turn, entry *struct {
	Role      string `json:"role"`
	CreatedAt int64  `json:"createdAt"`
	Content   []struct {
		Text string `json:"text"`
		Type string `json:"type"`
	} `json:"content"`
}) []Turn {
	if entry == nil {
		return turns
	}
	var text strings.Builder
	for _, block := range entry.Content {
		if block.Text != "" {
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(block.Text)
		}
	}
	if text.Len() == 0 {
		return turns
	}
	if entry.Role != "user" && entry.Role != "assistant" {
		return turns
	}
	return append(turns, Turn{
		Role:      entry.Role,
		Text:      text.String(),
		Timestamp: time.UnixMilli(entry.CreatedAt),
	})
}

// trackVibeTurnTiming accumulates per-turn duration and output tokens when a
// turn transitions from in_progress to completed.
func trackVibeTurnTiming(stats vibeJournalStats, turn *struct {
	ID          string `json:"id"`
	StartedAt   int64  `json:"startedAt"`
	CompletedAt int64  `json:"completedAt"`
	Status      string `json:"status"`
}, turnStart, turnOutStart map[string]int64) vibeJournalStats {
	if turn == nil || turn.ID == "" {
		return stats
	}
	switch turn.Status {
	case "in_progress", "running":
		if _, seen := turnStart[turn.ID]; !seen {
			turnStart[turn.ID] = turn.StartedAt
			turnOutStart[turn.ID] = stats.Usage.OutputTokens
		}
	case "completed":
		start, started := turnStart[turn.ID]
		outStart, outSeen := turnOutStart[turn.ID]
		if started && outSeen && turn.CompletedAt > start {
			duration := float64(turn.CompletedAt-start) / 1000
			out := stats.Usage.OutputTokens - outStart
			if out < 0 {
				out = 0
			}
			stats.TurnCount++
			stats.OutputTokens += out
			stats.DurationSecs += duration
			delete(turnStart, turn.ID)
			delete(turnOutStart, turn.ID)
		}
	}
	return stats
}

// IsVibeUnifiedSession reports whether sessionPath is a unified-store session.
func IsVibeUnifiedSession(sessionPath string) bool {
	info, err := os.Stat(sessionPath)
	if err != nil {
		return false
	}
	if !info.IsDir() {
		return false
	}
	_, ok := readVibeUnifiedMeta(sessionPath)
	return ok
}

// DetectLatestUnifiedSession finds the most recently active unified session
// for the target project directory.
func (v *VibeProvider) DetectLatestUnifiedSession(targetCwd string) (*Session, error) {
	sessions := v.listUnifiedSessions(targetCwd)
	if len(sessions) == 0 {
		return nil, fmt.Errorf("no vibe unified sessions found for %q", targetCwd)
	}
	latest := sessions[0]
	return &Session{
		ID:        latest.ID,
		Provider:  v.Name(),
		Path:      latest.Dir,
		CWD:       latest.CWD,
		UpdatedAt: latest.UpdatedAt,
	}, nil
}

// parseUnifiedSession builds SessionDetails from a unified-store session,
// using projection-state for token totals and the journal for turns.
func (v *VibeProvider) parseUnifiedSession(sessionDir string) (*SessionDetails, error) {
	info, err := os.Stat(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("accessing vibe session %q: %w", sessionDir, err)
	}
	meta, _ := readVibeUnifiedMeta(sessionDir)

	details := &SessionDetails{
		Session: Session{
			ID:        meta.SessionID,
			Provider:  v.Name(),
			Path:      sessionDir,
			CWD:       meta.Environment.WorkingDirectory,
			UpdatedAt: info.ModTime(),
		},
		Turns: make([]Turn, 0),
	}

	model := FormatModelName(meta.Config.ActiveModel)
	if model == "" {
		model = FormatModelName(meta.Config.ModelName)
	}
	if model != "" {
		details.ModelsUsed = append(details.ModelsUsed, model)
	}

	stats := scanVibeJournal(sessionDir)
	for i := range stats.Turns {
		stats.Turns[i].Model = model
		if stats.Turns[i].Role == "user" {
			details.LastUserMessage = stats.Turns[i].Text
		} else {
			details.LastAssistantMessage = stats.Turns[i].Text
		}
	}
	details.Turns = stats.Turns

	if usage, ok := readVibeProjectionUsage(sessionDir); ok {
		details.TotalTokens = usage.tokens()
	} else {
		details.TotalTokens = stats.Usage.tokens()
	}
	return details, nil
}

// collectUnifiedUsage aggregates usage telemetry over the unified store.
func (v *VibeProvider) collectUnifiedUsage(now time.Time, projectDir ...string) UsageMetrics {
	metrics := UsageMetrics{ModelStats: make(map[string]ModelMetric)}

	var cleanTarget string
	if len(projectDir) > 0 && projectDir[0] != "" {
		cleanTarget = filepath.Clean(projectDir[0])
	}

	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	startOfWeek := startOfDay.AddDate(0, 0, -7)

	var (
		totalOutTok     int64
		totalDuration   float64
		latestModelTime time.Time
		uniqueModels    = make(map[string]bool)
	)

	for _, s := range v.listUnifiedSessions(cleanTarget) {
		metrics.TotalSessions++

		model := s.Model
		if model == "" {
			model = "unknown"
		}
		uniqueModels[model] = true

		usage, ok := readVibeProjectionUsage(s.Dir)
		if !ok {
			// Session may be mid-turn without a projection snapshot yet;
			// fall back to the last usage envelope recorded in the journal.
			usage = scanVibeJournal(s.Dir).Usage
		}
		tokens := usage.tokens()
		metrics.TotalTokens += tokens

		t := s.UpdatedAt
		isWeek := t.After(startOfWeek) || t.Equal(startOfWeek)
		if isWeek {
			metrics.TokensThisWeek += tokens
		}
		isToday := t.After(startOfDay) || t.Equal(startOfDay)
		if isToday {
			metrics.TokensToday += tokens
		}

		m := metrics.ModelStats[model]
		m.Model = model
		m.TotalTokens += tokens
		if isWeek {
			m.TokensThisWeek += tokens
		}
		if isToday {
			m.TokensToday += tokens
		}

		// Speed is a live metric: only unfold journals of recent sessions.
		if isWeek {
			jStats := scanVibeJournal(s.Dir)
			m.Turns += jStats.TurnCount
			if jStats.DurationSecs > 0 && jStats.OutputTokens > 0 {
				m.OutputTokens += jStats.OutputTokens
				m.DurationSecs += jStats.DurationSecs
				totalOutTok += jStats.OutputTokens
				totalDuration += jStats.DurationSecs
			}
		}
		metrics.ModelStats[model] = m

		if t.After(latestModelTime) {
			metrics.LastModel = model
			latestModelTime = t
		}
	}

	for model, ms := range metrics.ModelStats {
		if ms.DurationSecs > 0 {
			ms.TokensPerSec = float64(ms.OutputTokens) / ms.DurationSecs
			metrics.ModelStats[model] = ms
		}
	}
	if totalDuration > 0 {
		metrics.TokensPerSec = float64(totalOutTok) / totalDuration
	}

	for m := range uniqueModels {
		metrics.UniqueModels = append(metrics.UniqueModels, m)
	}
	sort.Strings(metrics.UniqueModels)

	return metrics
}

// mergeUsageMetrics combines metrics from disjoint session stores.
func mergeUsageMetrics(a, b UsageMetrics) UsageMetrics {
	merged := a
	if merged.ModelStats == nil {
		merged.ModelStats = make(map[string]ModelMetric)
	}

	merged.TotalSessions += b.TotalSessions
	merged.TotalTokens += b.TotalTokens
	merged.TokensToday += b.TokensToday
	merged.TokensThisWeek += b.TokensThisWeek
	merged.TotalCostUSD += b.TotalCostUSD

	totalOutTok := int64(0)
	totalDuration := 0.0
	for model, bm := range b.ModelStats {
		am := merged.ModelStats[model]
		am.Model = model
		am.Turns += bm.Turns
		am.TokensToday += bm.TokensToday
		am.TokensThisWeek += bm.TokensThisWeek
		am.TotalTokens += bm.TotalTokens
		am.OutputTokens += bm.OutputTokens
		am.DurationSecs += bm.DurationSecs
		am.CostUSD += bm.CostUSD
		if am.DurationSecs > 0 {
			am.TokensPerSec = float64(am.OutputTokens) / am.DurationSecs
		}
		merged.ModelStats[model] = am
	}
	for _, ms := range merged.ModelStats {
		totalOutTok += ms.OutputTokens
		totalDuration += ms.DurationSecs
	}
	// Recompute aggregate speed from merged per-model figures so both stores
	// contribute with consistent weighting.
	if totalDuration > 0 {
		merged.TokensPerSec = float64(totalOutTok) / totalDuration
	}

	// The unified store is the live one; it wins for the most recent model
	// whenever it recorded any session.
	if b.LastModel != "" {
		merged.LastModel = b.LastModel
	}

	seen := make(map[string]bool)
	for _, m := range merged.UniqueModels {
		seen[m] = true
	}
	for _, m := range b.UniqueModels {
		if !seen[m] {
			merged.UniqueModels = append(merged.UniqueModels, m)
			seen[m] = true
		}
	}
	sort.Strings(merged.UniqueModels)

	return merged
}
