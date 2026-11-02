package provider

import (
	"time"
)

// Turn represents a single exchange or message turn in an AI agent session.
type Turn struct {
	Role      string    `json:"role"` // "user" or "assistant"
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
	Tokens    int       `json:"tokens,omitempty"`
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
	TotalTokens          int    `json:"total_tokens"`
	LastUserMessage      string `json:"last_user_message"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

// Provider defines the interface for interacting with an official AI CLI.
type Provider interface {
	Name() string
	BinaryName() string
	DetectLatestSession(cwd string) (*Session, error)
	ParseSession(sessionPath string) (*SessionDetails, error)
	FormatExecArgs(prompt string) []string
	DefaultCooldown() time.Duration
}
