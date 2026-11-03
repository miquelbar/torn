package stats

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"torn/internal/cooldown"
	"torn/internal/provider"
)

type modelExportEntry struct {
	Provider string
	Metric   provider.ModelMetric
}

func sortedModelEntries(statsList []ProviderStats) []modelExportEntry {
	var allModels []modelExportEntry
	for _, ps := range statsList {
		for _, m := range ps.ModelStats {
			allModels = append(allModels, modelExportEntry{
				Provider: ps.Name,
				Metric:   m,
			})
		}
	}

	sort.Slice(allModels, func(i, j int) bool {
		if allModels[i].Metric.TotalTokens != allModels[j].Metric.TotalTokens {
			return allModels[i].Metric.TotalTokens > allModels[j].Metric.TotalTokens
		}
		return allModels[i].Metric.Turns > allModels[j].Metric.Turns
	})

	return allModels
}

// Render formats stats according to format: table, markdown/md, csv, json.
func Render(statsList []ProviderStats, format string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "table", "ansi", "text":
		return RenderDashboard(statsList), nil
	case "markdown", "md":
		return RenderMarkdown(statsList), nil
	case "csv":
		return RenderCSV(statsList)
	case "json":
		return RenderJSON(statsList)
	default:
		return "", fmt.Errorf("unsupported output format %q (supported: table, markdown, csv, json)", format)
	}
}

// RenderMarkdown produces clean GitHub Flavored Markdown tables.
func RenderMarkdown(statsList []ProviderStats) string {
	var sb strings.Builder

	sb.WriteString("# Torn Hypervisor Stats\n\n")
	sb.WriteString("| Provider | Status | Cooldown | Sessions | Model | Today | Total | Speed |\n")
	sb.WriteString("| :--- | :--- | :--- | ---: | :--- | ---: | ---: | ---: |\n")

	for _, ps := range statsList {
		statusStr := cleanStatus(ps.Status)

		cooldownStr := "—"
		if ps.CooldownTime > 0 {
			cooldownStr = cooldown.FormatDuration(ps.CooldownTime)
		}

		modelStr := "—"
		if ps.LastModel != "" {
			cleanModel := strings.TrimPrefix(ps.LastModel, "claude-")
			extra := len(ps.UniqueModels) - 1
			if extra > 0 {
				modelStr = fmt.Sprintf("%s (+%d)", cleanModel, extra)
			} else {
				modelStr = cleanModel
			}
		}

		speedStr := "—"
		if ps.TokensPerSec > 0 {
			speedStr = fmt.Sprintf("%.1f t/s", ps.TokensPerSec)
		}

		sb.WriteString(fmt.Sprintf("| %s | %s | %s | %d | %s | %s | %s | %s |\n",
			ps.Name,
			statusStr,
			cooldownStr,
			ps.SessionsCount,
			modelStr,
			formatNumber(ps.TokensToday),
			formatNumber(ps.TotalTokens),
			speedStr,
		))
	}

	allModels := sortedModelEntries(statsList)
	if len(allModels) > 0 {
		sb.WriteString("\n## Model Breakdown\n\n")
		sb.WriteString("| Provider | Model | Turns | Today | Total | Speed |\n")
		sb.WriteString("| :--- | :--- | ---: | ---: | ---: | ---: |\n")

		for _, entry := range allModels {
			speedStr := "—"
			if entry.Metric.TokensPerSec > 0 {
				speedStr = fmt.Sprintf("%.1f t/s", entry.Metric.TokensPerSec)
			}

			cleanModel := strings.TrimPrefix(entry.Metric.Model, "claude-")

			sb.WriteString(fmt.Sprintf("| %s | %s | %d | %s | %s | %s |\n",
				entry.Provider,
				cleanModel,
				entry.Metric.Turns,
				formatNumber(entry.Metric.TokensToday),
				formatNumber(entry.Metric.TotalTokens),
				speedStr,
			))
		}
	}

	return sb.String()
}

// RenderCSV exports RFC 4180 compliant CSV.
func RenderCSV(statsList []ProviderStats) (string, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	if err := writer.Write([]string{
		"Provider", "Status", "Cooldown", "Sessions", "Model", "Today", "Total", "Speed",
	}); err != nil {
		return "", err
	}

	for _, ps := range statsList {
		statusStr := cleanStatus(ps.Status)
		cooldownStr := ""
		if ps.CooldownTime > 0 {
			cooldownStr = cooldown.FormatDuration(ps.CooldownTime)
		}
		speedStr := ""
		if ps.TokensPerSec > 0 {
			speedStr = fmt.Sprintf("%.1f", ps.TokensPerSec)
		}

		modelStr := ps.LastModel
		if modelStr != "" && len(ps.UniqueModels) > 1 {
			modelStr = fmt.Sprintf("%s (+%d)", strings.TrimPrefix(modelStr, "claude-"), len(ps.UniqueModels)-1)
		}

		row := []string{
			ps.Name,
			statusStr,
			cooldownStr,
			strconv.Itoa(ps.SessionsCount),
			modelStr,
			strconv.FormatInt(ps.TokensToday, 10),
			strconv.FormatInt(ps.TotalTokens, 10),
			speedStr,
		}
		if err := writer.Write(row); err != nil {
			return "", err
		}
	}

	allModels := sortedModelEntries(statsList)
	if len(allModels) > 0 {
		writer.Flush()
		buf.WriteString("\n")

		if err := writer.Write([]string{
			"Provider", "Model", "Turns", "Today", "Total", "Speed",
		}); err != nil {
			return "", err
		}

		for _, entry := range allModels {
			speedStr := ""
			if entry.Metric.TokensPerSec > 0 {
				speedStr = fmt.Sprintf("%.1f", entry.Metric.TokensPerSec)
			}

			row := []string{
				entry.Provider,
				entry.Metric.Model,
				strconv.Itoa(entry.Metric.Turns),
				strconv.FormatInt(entry.Metric.TokensToday, 10),
				strconv.FormatInt(entry.Metric.TotalTokens, 10),
				speedStr,
			}
			if err := writer.Write(row); err != nil {
				return "", err
			}
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// RenderJSON serializes the stats slice as pretty-printed JSON.
func RenderJSON(statsList []ProviderStats) (string, error) {
	data, err := json.MarshalIndent(statsList, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func cleanStatus(st cooldown.Status) string {
	switch st {
	case cooldown.StatusActive:
		return "ACTIVE"
	case cooldown.StatusCooldown:
		return "COOLDOWN"
	default:
		return "READY"
	}
}
