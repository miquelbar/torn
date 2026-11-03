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
	"torn/internal/strutil"
)

// ProviderStats aggregates quota, state, and usage metrics for a provider.
type ProviderStats struct {
	Name           string                          `json:"name"`
	Status         cooldown.Status                 `json:"status"`
	CooldownTime   time.Duration                   `json:"cooldown_time"`
	SessionsCount  int                             `json:"sessions_count"`
	LastModel      string                          `json:"last_model,omitempty"`
	UniqueModels   []string                        `json:"unique_models,omitempty"`
	TokensToday    int64                           `json:"tokens_today"`
	TokensThisWeek int64                           `json:"tokens_this_week"`
	TotalTokens    int64                           `json:"total_tokens"`
	TokensPerSec   float64                         `json:"tokens_per_sec"`
	BurnRatePerMin float64                         `json:"burn_rate_per_min"`
	ModelStats     map[string]provider.ModelMetric `json:"model_stats,omitempty"`
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

	divider := "\033[90m" + strings.Repeat("─", 94) + "\033[0m\n"

	sb.WriteString("\n\033[1mTORN HYPERVISOR STATS\033[0m\n")
	sb.WriteString(divider)
	sb.WriteString(fmt.Sprintf("\033[90m%-10s %-12s %-10s %-9s %-22s %-22s %-9s\033[0m\n",
		"PROVIDER", "STATUS", "COOLDOWN", "SESSIONS", "MODEL", "TOKENS (TODAY / TOTAL)", "SPEED"))
	sb.WriteString(divider)

	type modelEntry struct {
		Provider string
		Metric   provider.ModelMetric
	}
	var allModels []modelEntry

	for _, ps := range statsList {
		statusCol := formatStatus(ps.Status, 12)

		cooldownCol := "\033[90m—         \033[0m"
		if ps.CooldownTime > 0 {
			durStr := cooldown.FormatDuration(ps.CooldownTime)
			cooldownCol = fmt.Sprintf("\033[31m%-10s\033[0m", durStr)
		}

		sessionsCol := fmt.Sprintf("%-9s", FormatCompactNumber(int64(ps.SessionsCount)))

		modelName := formatCompactModel(ps.LastModel, len(ps.UniqueModels)-1, 20)
		modelCol := fmt.Sprintf("%-22s", modelName)

		todayStr := FormatCompactNumber(ps.TokensToday)
		totalStr := FormatCompactNumber(ps.TotalTokens)
		tokensStr := fmt.Sprintf("%s / %s", todayStr, totalStr)
		tokensCol := fmt.Sprintf("%-22s", tokensStr)

		speedCol := "\033[90m—        \033[0m"
		if ps.TokensPerSec > 0 {
			speedCol = fmt.Sprintf("%-9s", fmt.Sprintf("%.1f t/s", ps.TokensPerSec))
		}

		sb.WriteString(fmt.Sprintf("\033[1m%-10s\033[0m %s %s %s %s %s %s\n",
			ps.Name,
			statusCol,
			cooldownCol,
			sessionsCol,
			modelCol,
			tokensCol,
			speedCol,
		))

		for _, m := range ps.ModelStats {
			allModels = append(allModels, modelEntry{
				Provider: ps.Name,
				Metric:   m,
			})
		}
	}
	sb.WriteString(divider)

	if len(allModels) > 0 {
		sort.Slice(allModels, func(i, j int) bool {
			if allModels[i].Metric.TotalTokens != allModels[j].Metric.TotalTokens {
				return allModels[i].Metric.TotalTokens > allModels[j].Metric.TotalTokens
			}
			return allModels[i].Metric.Turns > allModels[j].Metric.Turns
		})

		subDivider := "\033[90m" + strings.Repeat("─", 80) + "\033[0m\n"

		sb.WriteString("\n\033[1mMODEL BREAKDOWN\033[0m\n")
		sb.WriteString(subDivider)
		sb.WriteString(fmt.Sprintf("\033[90m%-10s %-24s %-9s %-12s %-12s %-9s\033[0m\n",
			"PROVIDER", "MODEL", "TURNS", "TODAY", "TOTAL", "SPEED"))
		sb.WriteString(subDivider)

		for _, entry := range allModels {
			speed := "\033[90m—        \033[0m"
			if entry.Metric.TokensPerSec > 0 {
				speed = fmt.Sprintf("%-9s", fmt.Sprintf("%.1f t/s", entry.Metric.TokensPerSec))
			}

			modelName := strings.TrimPrefix(entry.Metric.Model, "claude-")
			modelName = strutil.Truncate(modelName, 23, "")

			sb.WriteString(fmt.Sprintf("\033[1m%-10s\033[0m %-24s %-9s %-12s %-12s %s\n",
				entry.Provider,
				modelName,
				FormatCompactNumber(int64(entry.Metric.Turns)),
				FormatCompactNumber(entry.Metric.TokensToday),
				FormatCompactNumber(entry.Metric.TotalTokens),
				speed,
			))
		}
		sb.WriteString(subDivider)
	}

	return sb.String()
}

func formatStatus(st cooldown.Status, width int) string {
	switch st {
	case cooldown.StatusActive:
		pad := width - 8
		if pad < 0 {
			pad = 0
		}
		return "\033[33m● ACTIVE\033[0m" + strings.Repeat(" ", pad)
	case cooldown.StatusCooldown:
		pad := width - 10
		if pad < 0 {
			pad = 0
		}
		return "\033[31m● COOLDOWN\033[0m" + strings.Repeat(" ", pad)
	default:
		pad := width - 7
		if pad < 0 {
			pad = 0
		}
		return "\033[32m● READY\033[0m" + strings.Repeat(" ", pad)
	}
}

func formatCompactModel(model string, extraCount int, maxLen int) string {
	if model == "" {
		return "—"
	}
	clean := strings.TrimPrefix(model, "claude-")

	if extraCount > 0 {
		suffix := fmt.Sprintf(" (+%d)", extraCount)
		avail := maxLen - len(suffix)
		if avail < 4 {
			avail = 4
		}
		return strutil.Truncate(clean, avail, "") + suffix
	}

	return strutil.Truncate(clean, maxLen, "")
}

// FormatCompactNumber formats large integers into readable forms like 119.5M, 2.5K, or —.
func FormatCompactNumber(n int64) string {
	if n <= 0 {
		return "—"
	}
	if n >= 1_000_000_000 {
		v := float64(n) / 1_000_000_000.0
		str := fmt.Sprintf("%.1fB", v)
		return strings.Replace(str, ".0B", "B", 1)
	}
	if n >= 1_000_000 {
		v := float64(n) / 1_000_000.0
		str := fmt.Sprintf("%.1fM", v)
		return strings.Replace(str, ".0M", "M", 1)
	}
	if n >= 1_000 {
		v := float64(n) / 1_000.0
		str := fmt.Sprintf("%.1fK", v)
		return strings.Replace(str, ".0K", "K", 1)
	}
	return fmt.Sprintf("%d", n)
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
