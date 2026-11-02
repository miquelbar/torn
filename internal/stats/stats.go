package stats

import (
	"fmt"
	"strings"
	"time"

	"torn/internal/cooldown"
	"torn/internal/provider"
	"torn/internal/storage"
)

// ProviderStats aggregates quota, state, and usage metrics for a provider.
type ProviderStats struct {
	Name           string
	Status         cooldown.Status
	CooldownTime   time.Duration
	SessionsCount  int
	TokensToday    int64
	TokensThisWeek int64
	TotalTokens    int64
	BurnRatePerMin float64
}

// Collector aggregates stats across all configured providers.
type Collector struct {
	registry *provider.Registry
	cooldown *cooldown.Manager
}

// NewCollector creates a stats Collector.
func NewCollector(reg *provider.Registry, cd *cooldown.Manager) *Collector {
	return &Collector{
		registry: reg,
		cooldown: cd,
	}
}

// Collect returns status and real usage metrics for all registered providers.
func (c *Collector) Collect(state *storage.State, now time.Time) []ProviderStats {
	all := c.registry.All()
	results := make([]ProviderStats, 0, len(all))

	for _, p := range all {
		status, remaining := c.cooldown.GetStatus(p.Name(), state, now)

		usage, _ := p.CollectUsage(now)

		burnRate := 0.0
		if usage.TokensToday > 0 {
			midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
			minsToday := now.Sub(midnight).Minutes()
			if minsToday > 0 {
				burnRate = float64(usage.TokensToday) / minsToday
			}
		}

		ps := ProviderStats{
			Name:           p.Name(),
			Status:         status,
			CooldownTime:   remaining,
			SessionsCount:  usage.TotalSessions,
			TokensToday:    usage.TokensToday,
			TokensThisWeek: usage.TokensThisWeek,
			TotalTokens:    usage.TotalTokens,
			BurnRatePerMin: burnRate,
		}
		results = append(results, ps)
	}

	return results
}

// RenderDashboard prints an ANSI formatted dashboard of provider statuses.
func RenderDashboard(statsList []ProviderStats) string {
	var sb strings.Builder

	sb.WriteString("===================================================================================================================\n")
	sb.WriteString("                                               TORN HYPERVISOR STATS                                               \n")
	sb.WriteString("===================================================================================================================\n")
	sb.WriteString(fmt.Sprintf("%-10s | %-14s | %-10s | %-10s | %-14s | %-14s | %-16s\n",
		"PROVIDER", "STATUS", "COOLDOWN", "SESSIONS", "TOKENS TODAY", "THIS WEEK", "TOTAL TOKENS"))
	sb.WriteString("-----------+----------------+------------+------------+----------------+----------------+------------------\n")

	for _, ps := range statsList {
		statusIcon := "🟢 READY"
		if ps.Status == cooldown.StatusActive {
			statusIcon = "🟡 ACTIVE"
		} else if ps.Status == cooldown.StatusCooldown {
			statusIcon = "🔴 COOLDOWN"
		}

		cooldownStr := "--"
		if ps.CooldownTime > 0 {
			cooldownStr = cooldown.FormatDuration(ps.CooldownTime)
		}

		sessionsStr := "--"
		if ps.SessionsCount > 0 {
			sessionsStr = formatNumber(int64(ps.SessionsCount))
		}

		sb.WriteString(fmt.Sprintf("%-10s | %-14s | %-10s | %-10s | %-14s | %-14s | %-16s\n",
			ps.Name,
			statusIcon,
			cooldownStr,
			sessionsStr,
			formatNumber(ps.TokensToday),
			formatNumber(ps.TokensThisWeek),
			formatNumber(ps.TotalTokens),
		))
	}
	sb.WriteString("===================================================================================================================\n")

	return sb.String()
}

func formatNumber(n int64) string {
	if n == 0 {
		return "--"
	}

	str := fmt.Sprintf("%d", n)
	if len(str) <= 3 {
		return str
	}

	var parts []string
	rem := len(str) % 3
	if rem > 0 {
		parts = append(parts, str[:rem])
	}
	for i := rem; i < len(str); i += 3 {
		parts = append(parts, str[i:i+3])
	}

	return strings.Join(parts, ",")
}
