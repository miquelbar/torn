package provider

import (
	"time"
)

// Turn represents a single exchange or message turn in an AI agent session.
type Turn struct {
	Role         string    `json:"role"` // "user" or "assistant"
	Text         string    `json:"text"`
	Tools        []string  `json:"tools,omitempty"`
	FilesTouched []string  `json:"files_touched,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
	Tokens       int       `json:"tokens,omitempty"`
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
	Turns                []Turn `json:"turns"`
	TotalTokens          int64  `json:"total_tokens"`
	LastUserMessage      string `json:"last_user_message"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

// UsageMetrics holds calculated token counts and rates for a provider.
type UsageMetrics struct {
	TotalSessions  int     `json:"total_sessions"`
	TokensToday    int64   `json:"tokens_today"`
	TokensThisWeek int64   `json:"tokens_this_week"`
	TotalTokens    int64   `json:"total_tokens"`
	BurnRatePerMin float64 `json:"burn_rate_per_min"`
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
