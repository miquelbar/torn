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
	TokensToday    int
	TokensThisWeek int
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
			// Estimate burn rate based on minutes elapsed today (or min 60 mins)
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
			BurnRatePerMin: burnRate,
		}
		results = append(results, ps)
	}

	return results
}

// RenderDashboard prints an ANSI formatted dashboard of provider statuses.
func RenderDashboard(statsList []ProviderStats) string {
	var sb strings.Builder

	sb.WriteString("========================================================================================\n")
	sb.WriteString("                                 TORN HYPERVISOR STATS                                  \n")
	sb.WriteString("========================================================================================\n")
	sb.WriteString(fmt.Sprintf("%-10s | %-14s | %-12s | %-12s | %-12s | %-12s\n",
		"PROVIDER", "STATUS", "COOLDOWN", "SESSIONS", "TOKENS TODAY", "THIS WEEK"))
	sb.WriteString("-----------+----------------+--------------+--------------+--------------+--------------\n")

	for _, ps := range statsList {
		statusIcon := "🟢 LIBRE"
		if ps.Status == cooldown.StatusInUse {
			statusIcon = "🟡 EN USO"
		} else if ps.Status == cooldown.StatusRateLimited {
			statusIcon = "🔴 BLOQUEADO"
		}

		cooldownStr := "--"
		if ps.CooldownTime > 0 {
			cooldownStr = cooldown.FormatDuration(ps.CooldownTime)
		}

		tokensTodayStr := fmt.Sprintf("%d", ps.TokensToday)
		if ps.TokensToday == 0 {
			tokensTodayStr = "--"
		}

		tokensWeekStr := fmt.Sprintf("%d", ps.TokensThisWeek)
		if ps.TokensThisWeek == 0 {
			tokensWeekStr = "--"
		}

		sessionsStr := fmt.Sprintf("%d", ps.SessionsCount)
		if ps.SessionsCount == 0 {
			sessionsStr = "--"
		}

		sb.WriteString(fmt.Sprintf("%-10s | %-14s | %-12s | %-12s | %-12s | %-12s\n",
			ps.Name,
			statusIcon,
			cooldownStr,
			sessionsStr,
			tokensTodayStr,
			tokensWeekStr,
		))
	}
	sb.WriteString("========================================================================================\n")

	return sb.String()
}
