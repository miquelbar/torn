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

// Collect returns status and metrics for all registered providers.
func (c *Collector) Collect(state *storage.State, now time.Time) []ProviderStats {
	all := c.registry.All()
	results := make([]ProviderStats, 0, len(all))

	for _, p := range all {
		status, remaining := c.cooldown.GetStatus(p.Name(), state, now)

		// Calculate simulated or parsed token metrics
		ps := ProviderStats{
			Name:           p.Name(),
			Status:         status,
			CooldownTime:   remaining,
			TokensToday:    0,
			TokensThisWeek: 0,
			BurnRatePerMin: 0,
		}
		results = append(results, ps)
	}

	return results
}

// RenderDashboard prints an ANSI formatted dashboard of provider statuses.
func RenderDashboard(statsList []ProviderStats) string {
	var sb strings.Builder

	sb.WriteString("====================================================================\n")
	sb.WriteString("                       TORN HYPERVISOR STATS                        \n")
	sb.WriteString("====================================================================\n")
	sb.WriteString(fmt.Sprintf("%-12s | %-16s | %-14s | %-12s\n", "PROVIDER", "STATUS", "COOLDOWN", "BURN RATE"))
	sb.WriteString("-------------+------------------+----------------+--------------\n")

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

		burnStr := fmt.Sprintf("%.0f tok/m", ps.BurnRatePerMin)
		if ps.BurnRatePerMin == 0 {
			burnStr = "--"
		}

		sb.WriteString(fmt.Sprintf("%-12s | %-16s | %-14s | %-12s\n",
			ps.Name,
			statusIcon,
			cooldownStr,
			burnStr,
		))
	}
	sb.WriteString("====================================================================\n")

	return sb.String()
}
