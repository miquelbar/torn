package stats

import (
	"fmt"
	"sort"
	"strings"
	"sync"
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
	LastModel      string
	UniqueModels   []string
	TokensToday    int64
	TokensThisWeek int64
	TotalTokens    int64
	TokensPerSec   float64
	BurnRatePerMin float64
	ModelStats     map[string]provider.ModelMetric
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

// Collect returns status and real usage metrics for all registered providers concurrently.
func (c *Collector) Collect(state *storage.State, now time.Time) []ProviderStats {
	all := c.registry.All()
	results := make([]ProviderStats, len(all))
	var wg sync.WaitGroup

	for i, p := range all {
		wg.Add(1)
		go func(idx int, prov provider.Provider) {
			defer wg.Done()

			status, remaining := c.cooldown.GetStatus(prov.Name(), state, now)
			usage, _ := prov.CollectUsage(now)

			burnRate := 0.0
			if usage.TokensToday > 0 {
				midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
				minsToday := now.Sub(midnight).Minutes()
				if minsToday > 0 {
					burnRate = float64(usage.TokensToday) / minsToday
				}
			}

			results[idx] = ProviderStats{
				Name:           prov.Name(),
				Status:         status,
				CooldownTime:   remaining,
				SessionsCount:  usage.TotalSessions,
				LastModel:      usage.LastModel,
				UniqueModels:   usage.UniqueModels,
				TokensToday:    usage.TokensToday,
				TokensThisWeek: usage.TokensThisWeek,
				TotalTokens:    usage.TotalTokens,
				TokensPerSec:   usage.TokensPerSec,
				BurnRatePerMin: burnRate,
				ModelStats:     usage.ModelStats,
			}
		}(i, p)
	}

	wg.Wait()
	return results
}

// RenderDashboard prints an ANSI formatted dashboard of provider statuses and model breakdown.
func RenderDashboard(statsList []ProviderStats) string {
	var sb strings.Builder

	borderWidth := 128
	sep := strings.Repeat("=", borderWidth) + "\n"

	sb.WriteString(sep)
	sb.WriteString(center("TORN HYPERVISOR STATS", borderWidth) + "\n")
	sb.WriteString(sep)
	sb.WriteString(fmt.Sprintf("%-10s | %-14s | %-10s | %-8s | %-25s | %-14s | %-14s | %-14s | %-10s\n",
		"PROVIDER", "STATUS", "COOLDOWN", "SESSIONS", "LAST MODEL", "TOKENS TODAY", "THIS WEEK", "TOTAL TOKENS", "SPEED"))
	sb.WriteString("-----------+----------------+------------+----------+---------------------------+----------------+----------------+----------------+-----------\n")

	type modelEntry struct {
		Provider string
		Metric   provider.ModelMetric
	}
	var allModels []modelEntry

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

		modelStr := "--"
		if ps.LastModel != "" {
			modelStr = ps.LastModel
			if len(ps.UniqueModels) > 1 {
				modelStr = fmt.Sprintf("%s (+%d)", ps.LastModel, len(ps.UniqueModels)-1)
			}
		}

		speedStr := "--"
		if ps.TokensPerSec > 0 {
			speedStr = fmt.Sprintf("%.1f tok/s", ps.TokensPerSec)
		}

		sb.WriteString(fmt.Sprintf("%-10s | %-14s | %-10s | %-8s | %-25s | %-14s | %-14s | %-14s | %-10s\n",
			ps.Name,
			statusIcon,
			cooldownStr,
			sessionsStr,
			modelStr,
			formatNumber(ps.TokensToday),
			formatNumber(ps.TokensThisWeek),
			formatNumber(ps.TotalTokens),
			speedStr,
		))

		for _, m := range ps.ModelStats {
			allModels = append(allModels, modelEntry{
				Provider: ps.Name,
				Metric:   m,
			})
		}
	}
	sb.WriteString(sep)

	if len(allModels) > 0 {
		sort.Slice(allModels, func(i, j int) bool {
			if allModels[i].Metric.TotalTokens != allModels[j].Metric.TotalTokens {
				return allModels[i].Metric.TotalTokens > allModels[j].Metric.TotalTokens
			}
			return allModels[i].Metric.Turns > allModels[j].Metric.Turns
		})

		sb.WriteString("\n")
		sb.WriteString(sep)
		sb.WriteString(center("MODEL BREAKDOWN", borderWidth) + "\n")
		sb.WriteString(sep)
		sb.WriteString(fmt.Sprintf("%-10s | %-28s | %-10s | %-14s | %-16s | %-14s\n",
			"PROVIDER", "MODEL", "TURNS", "TOKENS TODAY", "TOTAL TOKENS", "SPEED (TOK/S)"))
		sb.WriteString("-----------+------------------------------+------------+----------------+------------------+---------------\n")

		for _, entry := range allModels {
			speed := "--"
			if entry.Metric.TokensPerSec > 0 {
				speed = fmt.Sprintf("%.1f", entry.Metric.TokensPerSec)
			}

			sb.WriteString(fmt.Sprintf("%-10s | %-28s | %-10s | %-14s | %-16s | %-14s\n",
				entry.Provider,
				entry.Metric.Model,
				formatNumber(int64(entry.Metric.Turns)),
				formatNumber(entry.Metric.TokensToday),
				formatNumber(entry.Metric.TotalTokens),
				speed,
			))
		}
		sb.WriteString(sep)
	}

	return sb.String()
}

func center(s string, width int) string {
	if len(s) >= width {
		return s
	}
	left := (width - len(s)) / 2
	right := width - len(s) - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
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
