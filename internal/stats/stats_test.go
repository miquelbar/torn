package stats

import (
	"strings"
	"testing"
	"time"

	"torn/internal/cooldown"
	"torn/internal/provider"
	"torn/internal/storage"
)

func TestCollectAndRenderDashboard(t *testing.T) {
	reg := provider.DefaultRegistry()
	cd := cooldown.NewManager()
	collector := NewCollector(reg, cd)

	now := time.Now()
	state := &storage.State{
		ActiveCLI: "claude",
		Cooldowns: map[string]time.Time{
			"codex": now.Add(90 * time.Minute),
		},
	}

	statsList := collector.Collect(state, now)
	if len(statsList) < 4 {
		t.Fatalf("expected at least 4 providers in stats, got %d", len(statsList))
	}

	output := RenderDashboard(statsList)

	if !strings.Contains(output, "TORN HYPERVISOR STATS") {
		t.Errorf("output missing title")
	}
	if !strings.Contains(output, "TOTAL TOKENS") {
		t.Errorf("output missing TOTAL TOKENS header")
	}
	if !strings.Contains(output, "🟡 ACTIVE") {
		t.Errorf("output missing ACTIVE status for claude: %s", output)
	}
	if !strings.Contains(output, "🔴 COOLDOWN") {
		t.Errorf("output missing COOLDOWN status for codex: %s", output)
	}
	if !strings.Contains(output, "🟢 READY") {
		t.Errorf("output missing READY status for available providers: %s", output)
	}
}

func TestFormatNumber(t *testing.T) {
	if formatNumber(0) != "--" {
		t.Errorf("expected '--', got %q", formatNumber(0))
	}
	if formatNumber(999) != "999" {
		t.Errorf("expected '999', got %q", formatNumber(999))
	}
	if formatNumber(1000) != "1,000" {
		t.Errorf("expected '1,000', got %q", formatNumber(1000))
	}
	if formatNumber(1849939873) != "1,849,939,873" {
		t.Errorf("expected '1,849,939,873', got %q", formatNumber(1849939873))
	}
}

func TestRenderDashboardWithModelsAndSpeed(t *testing.T) {
	statsList := []ProviderStats{
		{
			Name:          "claude",
			Status:        cooldown.StatusActive,
			SessionsCount: 5,
			LastModel:     "claude-sonnet-4-5",
			UniqueModels:  []string{"claude-opus-4-6", "claude-sonnet-4-5"},
			TokensToday:   12000,
			TotalTokens:   150000,
			TokensPerSec:  52.4,
			ModelStats: map[string]provider.ModelMetric{
				"claude-opus-4-6": {
					Model:        "claude-opus-4-6",
					Turns:        10,
					TotalTokens:  50000,
					TokensToday:  2000,
					TokensPerSec: 35.0,
				},
				"claude-sonnet-4-5": {
					Model:        "claude-sonnet-4-5",
					Turns:        25,
					TotalTokens:  100000,
					TokensToday:  10000,
					TokensPerSec: 58.2,
				},
			},
		},
	}

	output := RenderDashboard(statsList)

	if !strings.Contains(output, "claude-sonnet-4-5 (+1)") {
		t.Errorf("expected model with (+1) suffix, got:\n%s", output)
	}
	if !strings.Contains(output, "52.4 tok/s") {
		t.Errorf("expected 52.4 tok/s speed in main table, got:\n%s", output)
	}
	if !strings.Contains(output, "MODEL BREAKDOWN") {
		t.Errorf("expected MODEL BREAKDOWN sub-table, got:\n%s", output)
	}
	if !strings.Contains(output, "claude-sonnet-4-5") || !strings.Contains(output, "claude-opus-4-6") {
		t.Errorf("expected both models in breakdown table, got:\n%s", output)
	}
}
