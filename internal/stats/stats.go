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
	TotalCostUSD   float64                         `json:"total_cost_usd,omitempty"`
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

// DashboardOptions configures the title and scope for rendering dashboards.
type DashboardOptions struct {
	Scope       string // "project" or "global"
	ProjectName string // e.g. "torn"
}

// Collect returns status and real usage metrics for all registered providers concurrently.
// If projectPath is provided, metrics are scoped strictly to that project directory.
func (c *Collector) Collect(state *storage.State, now time.Time, projectPath ...string) []ProviderStats {
	all := c.registry.All()
	results := make([]ProviderStats, len(all))
	var wg sync.WaitGroup

	for i, p := range all {
		wg.Add(1)
		go func(idx int, prov provider.Provider) {
			defer wg.Done()

			status, remaining := c.cooldown.GetStatus(prov.Name(), state, now)
			usage, _ := prov.CollectUsage(now, projectPath...)

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
				TotalCostUSD:   usage.TotalCostUSD,
			}
		}(i, p)
	}

	wg.Wait()
	return results
}

// RenderDashboard prints an ANSI formatted dashboard of provider statuses with a hierarchical model tree.
func RenderDashboard(statsList []ProviderStats, opts ...DashboardOptions) string {
	var sb strings.Builder

	title := "TORN HYPERVISOR STATS (GLOBAL)"
	if len(opts) > 0 && opts[0].Scope == "project" {
		projName := opts[0].ProjectName
		if projName == "" {
			projName = "CURRENT PROJECT"
		}
		title = fmt.Sprintf("TORN PROJECT STATS (%s)", projName)
	}

	divider := "\033[90m" + strings.Repeat("─", 108) + "\033[0m\n"

	sb.WriteString("\n\033[1m" + title + "\033[0m\n")
	sb.WriteString(divider)
	sb.WriteString(fmt.Sprintf("\033[90m%-30s  %-12s  %8s  %8s  %9s  %9s  %9s  %9s\033[0m\n",
		"PROVIDER / MODEL", "STATUS", "COOLDOWN", "SESSIONS", "TODAY", "TOTAL", "COST", "SPEED"))
	sb.WriteString(divider)

	for _, ps := range statsList {
		statusCol := formatStatus(ps.Status, 12)

		var cooldownCol string
		if ps.CooldownTime > 0 {
			durStr := cooldown.FormatDuration(ps.CooldownTime)
			cooldownCol = padLeftColor(durStr, 8, "\033[31m")
		} else {
			cooldownCol = padLeft("—", 8)
		}

		sessionsCol := padLeft(FormatCompactNumber(int64(ps.SessionsCount)), 8)
		todayCol := padLeft(FormatCompactNumber(ps.TokensToday), 9)
		totalCol := padLeft(FormatCompactNumber(ps.TotalTokens), 9)
		costCol := padLeft(FormatCost(ps.TotalCostUSD), 9)

		var speedCol string
		if ps.TokensPerSec > 0 {
			speedCol = padLeft(fmt.Sprintf("%.1f t/s", ps.TokensPerSec), 9)
		} else {
			speedCol = padLeft("—", 9)
		}

		sb.WriteString(fmt.Sprintf("\033[1m%-30s\033[0m  %s  %s  %s  %s  %s  %s  %s\n",
			ps.Name,
			statusCol,
			cooldownCol,
			sessionsCol,
			todayCol,
			totalCol,
			costCol,
			speedCol,
		))

		// Render child model rows in a hierarchical tree
		if len(ps.ModelStats) > 0 {
			var models []provider.ModelMetric
			for _, m := range ps.ModelStats {
				models = append(models, m)
			}
			sort.Slice(models, func(i, j int) bool {
				if models[i].TotalTokens != models[j].TotalTokens {
					return models[i].TotalTokens > models[j].TotalTokens
				}
				return models[i].Turns > models[j].Turns
			})

			for i, m := range models {
				treeGlyph := "├─"
				if i == len(models)-1 {
					treeGlyph = "└─"
				}

				modelName := strings.TrimPrefix(m.Model, "claude-")
				modelName = strutil.Truncate(modelName, 25, "")
				modelLabel := fmt.Sprintf("  \033[90m%s\033[0m %s", treeGlyph, padRight(modelName, 25))

				mStatusCol := strings.Repeat(" ", 12)
				mCooldownCol := strings.Repeat(" ", 8)
				mTurnsCol := padLeft(FormatCompactNumber(int64(m.Turns)), 8)
				mTodayCol := padLeft(FormatCompactNumber(m.TokensToday), 9)
				mTotalCol := padLeft(FormatCompactNumber(m.TotalTokens), 9)
				mCostCol := padLeft(FormatCost(m.CostUSD), 9)

				var mSpeedCol string
				if m.TokensPerSec > 0 {
					mSpeedCol = padLeft(fmt.Sprintf("%.1f t/s", m.TokensPerSec), 9)
				} else {
					mSpeedCol = padLeft("—", 9)
				}

				sb.WriteString(fmt.Sprintf("%s  %s  %s  %s  %s  %s  %s  %s\n",
					modelLabel,
					mStatusCol,
					mCooldownCol,
					mTurnsCol,
					mTodayCol,
					mTotalCol,
					mCostCol,
					mSpeedCol,
				))
			}
		}
	}
	sb.WriteString(divider)

	return sb.String()
}

// FormatCost formats a USD amount into a clean compact form like $22.80, $0.03, $1.2K, or —.
func FormatCost(usd float64) string {
	if usd <= 0 {
		return "—"
	}
	if usd >= 1000 {
		return fmt.Sprintf("$%.1fK", usd/1000.0)
	}
	if usd < 0.01 {
		return fmt.Sprintf("$%.3f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	return s + strings.Repeat(" ", width-len(r))
}

func padLeft(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		return string(r[:width])
	}
	return strings.Repeat(" ", width-len(r)) + s
}

func padLeftColor(text string, width int, colorCode string) string {
	r := []rune(text)
	textLen := len(r)
	if textLen >= width {
		if colorCode != "" {
			return colorCode + text + "\033[0m"
		}
		return text
	}
	spaces := strings.Repeat(" ", width-textLen)
	if colorCode != "" {
		return spaces + colorCode + text + "\033[0m"
	}
	return spaces + text
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
	if n <= 0 {
		return "0"
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
