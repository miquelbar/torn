package provider

import (
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

	// Read log files inside directory or standalone file
	if stat.IsDir() {
		entries, err := os.ReadDir(sessionPath)
		if err == nil {
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".md") || strings.HasSuffix(entry.Name(), ".txt") {
					content, err := os.ReadFile(filepath.Join(sessionPath, entry.Name()))
					if err == nil && len(content) > 0 {
						details.Turns = append(details.Turns, Turn{
							Role:      "assistant",
							Text:      string(content),
							Timestamp: stat.ModTime(),
						})
						details.LastAssistantMessage = string(content)
					}
				}
			}
		}
	}

	return details, nil
}
