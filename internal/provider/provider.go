package provider

import (
	"time"
)

// ModelMetric records usage, turn counts, and speed for a specific model.
type ModelMetric struct {
	Model          string  `json:"model"`
	Turns          int     `json:"turns"`
	TokensToday    int64   `json:"tokens_today"`
	TokensThisWeek int64   `json:"tokens_this_week"`
	TotalTokens    int64   `json:"total_tokens"`
	OutputTokens   int64   `json:"output_tokens"`
	DurationSecs   float64 `json:"duration_secs"`
	TokensPerSec   float64 `json:"tokens_per_sec"`
}

// Turn represents a single exchange or message turn in an AI agent session.
type Turn struct {
	Role         string    `json:"role"` // "user" or "assistant"
	Text         string    `json:"text"`
	Tools        []string  `json:"tools,omitempty"`
	FilesTouched []string  `json:"files_touched,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
	Tokens       int       `json:"tokens,omitempty"`
	Model        string    `json:"model,omitempty"`
}

// Session represents a detected session file on disk.
type Session struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Path      string    `json:"path"`
	CWD       string    `json:"cwd"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SessionDetails contains parsed conversation turns and summary metadata.
type SessionDetails struct {
	Session
	Turns                []Turn     `json:"turns"`
	TotalTokens          int64      `json:"total_tokens"`
	LastUserMessage      string     `json:"last_user_message"`
	LastAssistantMessage string     `json:"last_assistant_message"`
	CooldownUntil        *time.Time `json:"cooldown_until,omitempty"`
	ModelsUsed           []string   `json:"models_used,omitempty"`
}

// UsageMetrics holds calculated token counts, models, and rates for a provider.
type UsageMetrics struct {
	TotalSessions  int                    `json:"total_sessions"`
	TokensToday    int64                  `json:"tokens_today"`
	TokensThisWeek int64                  `json:"tokens_this_week"`
	TotalTokens    int64                  `json:"total_tokens"`
	BurnRatePerMin float64                `json:"burn_rate_per_min"`
	LastModel      string                 `json:"last_model,omitempty"`
	UniqueModels   []string               `json:"unique_models,omitempty"`
	ModelStats     map[string]ModelMetric `json:"model_stats,omitempty"`
	TokensPerSec   float64                `json:"tokens_per_sec"`
}

// Provider defines the interface for interacting with an official AI CLI.
type Provider interface {
	Name() string
	BinaryName() string
	DetectLatestSession(cwd string) (*Session, error)
	ParseSession(sessionPath string) (*SessionDetails, error)
	FormatExecArgs(prompt string) []string
	DefaultCooldown() time.Duration
	CollectUsage(now time.Time) (UsageMetrics, error)
}
