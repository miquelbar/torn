package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"torn/internal/project"
)

// State represents the persistent runtime state of a project managed by torn.
type State struct {
	ProjectPath     string               `json:"project_path"`
	ProjectHash     string               `json:"project_hash"`
	ActiveCLI       string               `json:"active_cli,omitempty"`
	ActiveSessionID string               `json:"active_session_id,omitempty"`
	LastHandoff     time.Time            `json:"last_handoff,omitempty"`
	LastGitHead     string               `json:"last_git_head,omitempty"`
	RelayCount      int                  `json:"relay_count"`
	Cooldowns       map[string]time.Time `json:"cooldowns,omitempty"`
}

// HistoryEntry records a historical handoff or session switch event.
type HistoryEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	FromCLI     string    `json:"from_cli"`
	ToCLI       string    `json:"to_cli"`
	SessionID   string    `json:"session_id"`
	GitHead     string    `json:"git_head"`
	TurnsPassed int       `json:"turns_passed"`
	Dropped     int       `json:"dropped,omitempty"`
}

// Store handles isolated, zero-footprint storage operations for projects.
type Store struct {
	baseDir string
	mu      sync.RWMutex
}

// New creates a new Store rooted at baseDir.
// If baseDir is empty, it resolves to ~/.local/share/torn/projects.
func New(baseDir string) (*Store, error) {
	if baseDir == "" {
		resolved, err := defaultDataDir()
		if err != nil {
			return nil, err
		}
		baseDir = resolved
	}

	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("creating base storage dir %q: %w", baseDir, err)
	}

	return &Store{
		baseDir: baseDir,
	}, nil
}

// ProjectDir returns the isolated storage path for a given project info.
func (s *Store) ProjectDir(info *project.Info) string {
	return filepath.Join(s.baseDir, info.Hash)
}

// LoadState reads the State for a project. Returns default State if none exists yet.
func (s *Store) LoadState(info *project.Info) (*State, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := s.ProjectDir(info)
	statePath := filepath.Join(dir, "state.json")

	data, err := os.ReadFile(statePath)
	if os.IsNotExist(err) {
		return &State{
			ProjectPath: info.Path,
			ProjectHash: info.Hash,
			Cooldowns:   make(map[string]time.Time),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading state from %q: %w", statePath, err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decoding state %q: %w", statePath, err)
	}

	if state.Cooldowns == nil {
		state.Cooldowns = make(map[string]time.Time)
	}

	return &state, nil
}

// SaveState atomically writes the State for a project using POSIX-safe temp file rename.
func (s *Store) SaveState(info *project.Info, state *State) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.ProjectDir(info)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating project storage dir %q: %w", dir, err)
	}

	state.ProjectPath = info.Path
	state.ProjectHash = info.Hash

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	statePath := filepath.Join(dir, "state.json")
	tmpFile, err := os.CreateTemp(dir, "state-*.tmp")
	if err != nil {
		return fmt.Errorf("creating tmp state file: %w", err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		if tmpFile != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err := tmpFile.Chmod(0600); err != nil {
		return fmt.Errorf("setting tmp state file permissions: %w", err)
	}

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("writing tmp state file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("syncing tmp state file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing tmp state file: %w", err)
	}
	tmpFile = nil // Prevent defer from removing after successful close & rename

	if err := os.Rename(tmpName, statePath); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("atomic rename of state file: %w", err)
	}

	return nil
}

// AppendHistory appends a new record to the project's history.jsonl.
func (s *Store) AppendHistory(info *project.Info, entry *HistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.ProjectDir(info)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating project storage dir %q: %w", dir, err)
	}

	historyPath := filepath.Join(dir, "history.jsonl")
	f, err := os.OpenFile(historyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("opening history file %q: %w", historyPath, err)
	}
	defer f.Close()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling history entry: %w", err)
	}

	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("writing history entry: %w", err)
	}

	return nil
}

func defaultDataDir() (string, error) {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "torn", "projects"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving user home directory: %w", err)
	}

	return filepath.Join(home, ".local", "share", "torn", "projects"), nil
}
