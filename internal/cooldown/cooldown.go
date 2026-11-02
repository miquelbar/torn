package cooldown

import (
	"fmt"
	"time"

	"torn/internal/storage"
)

// Status represents the operational availability of an AI CLI.
type Status string

const (
	StatusReady    Status = "ready"    // 🟢 READY
	StatusActive   Status = "active"   // 🟡 ACTIVE
	StatusCooldown Status = "cooldown" // 🔴 COOLDOWN
)

// Manager handles rate-limit and availability tracking.
type Manager struct{}

// NewManager creates a cooldown Manager.
func NewManager() *Manager {
	return &Manager{}
}

// GetStatus checks provider availability based on project state and current time.
func (m *Manager) GetStatus(providerName string, state *storage.State, now time.Time) (Status, time.Duration) {
	if state.Cooldowns != nil {
		if until, ok := state.Cooldowns[providerName]; ok {
			if now.Before(until) {
				return StatusCooldown, until.Sub(now)
			}
		}
	}

	if state.ActiveCLI == providerName {
		return StatusActive, 0
	}

	return StatusReady, 0
}

// SetCooldown records a rate-limit cooldown for a provider in the state.
func (m *Manager) SetCooldown(providerName string, state *storage.State, duration time.Duration, now time.Time) {
	if state.Cooldowns == nil {
		state.Cooldowns = make(map[string]time.Time)
	}
	state.Cooldowns[providerName] = now.Add(duration)
}

// ClearCooldown removes any rate-limit cooldown for a provider.
func (m *Manager) ClearCooldown(providerName string, state *storage.State) {
	if state.Cooldowns != nil {
		delete(state.Cooldowns, providerName)
	}
}

// FormatDuration returns a clean human-readable countdown string (e.g., "1h 45m" or "42m").
func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %02dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
