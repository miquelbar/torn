package provider

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// agyQuotaScanAge bounds which stores are inspected for quota evidence.
	// Windows can be weekly (~140h), so files up to 7 days old are scanned.
	agyQuotaScanAge = 7 * 24 * time.Hour
	// agyQuotaDBStepWindow restricts database evidence to the final steps of
	// a conversation: quota errors happen when a session is cut short, and old
	// steps anchored to a fresh mtime would over-estimate the reset.
	agyQuotaDBStepWindow = 100
)

// agyQuotaStepRegex matches agy's quota-refusal payload embedded in the
// protobuf blobs of the conversations database ("steps" table):
// RESOURCE_EXHAUSTED (code 429): Individual quota reached. ... Resets in 4h8m33s.
var agyQuotaStepRegex = regexp.MustCompile(`(?i)RESOURCE_EXHAUSTED \(code 429\): Individual quota reached[^\x00]{0,160}?Resets in (?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?`)

// DetectQuotaCooldown returns the soonest active quota reset for the agy
// account. Antigravity quotas are per model group (Gemini, Claude/GPT,
// Argon) with separate weekly and five-hour windows, and the CLI becomes
// usable again when the first exhausted window refreshes; the soonest active
// reset across all evidence wins.
//
// Evidence sources, scanned across all projects because quotas attach to the
// account:
//   - brain transcripts: ERROR_MESSAGE steps carry exact created_at anchors.
//   - conversations/*.db: error steps persisted in the conversations database
//     even before the brain transcript is flushed; their payload blobs are
//     read through the sqlite3 CLI and anchored at the file's mtime.
func (a *AgyProvider) DetectQuotaCooldown(now time.Time) *time.Time {
	var soonest *time.Time
	consider := func(candidate time.Time) {
		if candidate.After(now) && (soonest == nil || candidate.Before(*soonest)) {
			soonest = &candidate
		}
	}

	pattern := filepath.Join(a.baseDir, "brain", "*", ".system_generated", "logs", "transcript.jsonl")
	for _, path := range recentAgyQuotaFiles(pattern, now, agyQuotaScanAge, 20) {
		for _, until := range scanAgyTranscriptQuota(path, now) {
			consider(until)
		}
	}

	dbPattern := filepath.Join(a.baseDir, "conversations", "*.db")
	for _, path := range recentAgyQuotaFiles(dbPattern, now, agyQuotaScanAge, 5) {
		for _, duration := range queryAgyDBQuotaResets(path) {
			consider(fileModTime(path, now).Add(duration))
		}
	}

	return soonest
}

// recentAgyQuotaFiles globs a pattern and returns its files ordered
// newest-first, keeping those modified within maxAge up to maxCount.
func recentAgyQuotaFiles(pattern string, now time.Time, maxAge time.Duration, maxCount int) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}

	type stamped struct {
		path string
		mt   time.Time
	}
	files := make([]stamped, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		if now.Sub(info.ModTime()) > maxAge {
			continue
		}
		files = append(files, stamped{path: path, mt: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].mt.After(files[j].mt)
	})

	paths := make([]string, 0, len(files))
	for _, f := range files[:min(len(files), maxCount)] {
		paths = append(paths, f.path)
	}
	return paths
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func fileModTime(path string, fallback time.Time) time.Time {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return fallback
}

// scanAgyTranscriptQuota extracts every active quota reset from one brain
// transcript, anchored at each step's exact created_at.
func scanAgyTranscriptQuota(path string, now time.Time) []time.Time {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var resets []time.Time
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, []byte(`"error"`)) {
			continue
		}
		var step struct {
			Error     string `json:"error"`
			CreatedAt string `json:"created_at"`
		}
		if err := json.Unmarshal(line, &step); err != nil || step.Error == "" {
			continue
		}
		msgTime := fileModTime(path, now)
		if step.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339Nano, step.CreatedAt); err == nil {
				msgTime = t
			} else if t, err := time.Parse(time.RFC3339, step.CreatedAt); err == nil {
				msgTime = t
			}
		}
		if until := DetectQuotaCooldown(step.Error, msgTime, now); until != nil {
			resets = append(resets, *until)
		}
	}
	return resets
}

// queryAgyDBQuotaResets reads quota-refusal windows from the conversations
// database. Only the final steps of each conversation are considered, and
// payloads are transferred as hex to survive the embedded protobuf binaries.
func queryAgyDBQuotaResets(dbPath string) []time.Duration {
	sqliteBin, err := exec.LookPath("sqlite3")
	if err != nil {
		return nil
	}

	query := fmt.Sprintf(
		"SELECT hex(step_payload) FROM steps WHERE idx >= (SELECT MAX(idx) - %d FROM steps) AND step_payload LIKE '%%Individual quota reached%%'",
		agyQuotaDBStepWindow,
	)
	cmd := exec.Command(sqliteBin, "-noheader", dbPath, query)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var durations []time.Duration
	for _, row := range strings.Split(string(out), "\n") {
		row = strings.TrimSpace(row)
		if row == "" {
			continue
		}
		raw, err := hex.DecodeString(row)
		if err != nil {
			continue
		}
		durations = append(durations, parseAgyQuotaWindows(raw)...)
	}
	return durations
}

// parseAgyQuotaWindows extracts every "Resets in XhYmZs" announcement from a
// step payload blob.
func parseAgyQuotaWindows(blob []byte) []time.Duration {
	var durations []time.Duration
	for _, m := range agyQuotaStepRegex.FindAllSubmatch(blob, -1) {
		hours, mins, secs := 0, 0, 0
		if len(m) > 1 && len(m[1]) > 0 {
			hours = atoiSafe(m[1])
		}
		if len(m) > 2 && len(m[2]) > 0 {
			mins = atoiSafe(m[2])
		}
		if len(m) > 3 && len(m[3]) > 0 {
			secs = atoiSafe(m[3])
		}
		d := time.Duration(hours)*time.Hour + time.Duration(mins)*time.Minute + time.Duration(secs)*time.Second
		if d > 0 {
			durations = append(durations, d)
		}
	}
	return durations
}

func atoiSafe(b []byte) int {
	n := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
