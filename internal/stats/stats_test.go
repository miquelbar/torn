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
		t.Errorf("output missing title: %s", output)
	}
	if !strings.Contains(output, "TODAY") || !strings.Contains(output, "TOTAL") {
		t.Errorf("output missing TODAY / TOTAL headers: %s", output)
	}
	if !strings.Contains(output, "● ACTIVE") {
		t.Errorf("output missing ACTIVE status for claude: %s", output)
	}
	if !strings.Contains(output, "● COOLDOWN") {
		t.Errorf("output missing COOLDOWN status for codex: %s", output)
	}
	if !strings.Contains(output, "● READY") {
		t.Errorf("output missing READY status for available providers: %s", output)
	}
}

func TestFormatCompactNumber(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "—"},
		{-10, "—"},
		{553, "553"},
		{1000, "1K"},
		{2500, "2.5K"},
		{100000, "100K"},
		{1200000, "1.2M"},
		{93485077, "93.5M"},
		{119500000, "119.5M"},
		{1849939873, "1.8B"},
	}

	for _, tc := range tests {
		got := FormatCompactNumber(tc.input)
		if got != tc.expected {
			t.Errorf("FormatCompactNumber(%d) = %q, want %q", tc.input, got, tc.expected)
		}
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
			TotalCostUSD:  14.50,
			TokensPerSec:  52.4,
			ModelStats: map[string]provider.ModelMetric{
				"claude-opus-4-6": {
					Model:        "claude-opus-4-6",
					Turns:        10,
					TotalTokens:  50000,
					TokensToday:  2000,
					CostUSD:      3.30,
					TokensPerSec: 35.0,
				},
				"claude-sonnet-4-5": {
					Model:        "claude-sonnet-4-5",
					Turns:        25,
					TotalTokens:  100000,
					TokensToday:  10000,
					CostUSD:      11.20,
					TokensPerSec: 58.2,
				},
			},
		},
	}

	output := RenderDashboard(statsList)

	if !strings.Contains(output, "sonnet-4-5") || !strings.Contains(output, "opus-4-6") {
		t.Errorf("expected both models in hierarchical tree, got:\n%s", output)
	}
	if !strings.Contains(output, "├─") || !strings.Contains(output, "└─") {
		t.Errorf("expected tree branch glyphs, got:\n%s", output)
	}
	if !strings.Contains(output, "52.4 t/s") {
		t.Errorf("expected 52.4 t/s speed in provider row, got:\n%s", output)
	}
	if !strings.Contains(output, "58.2 t/s") || !strings.Contains(output, "35.0 t/s") {
		t.Errorf("expected individual model speeds, got:\n%s", output)
	}
	if !strings.Contains(output, "$14.50") {
		t.Errorf("expected $14.50 provider cost, got:\n%s", output)
	}
	if !strings.Contains(output, "$11.20") || !strings.Contains(output, "$3.30") {
		t.Errorf("expected model costs, got:\n%s", output)
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		input    float64
		expected string
	}{
		{0.0, "—"},
		{-5.0, "—"},
		{0.005, "$0.005"},
		{0.03, "$0.03"},
		{22.80, "$22.80"},
		{1250.00, "$1.2K"},
	}

	for _, tc := range tests {
		got := FormatCost(tc.input)
		if got != tc.expected {
			t.Errorf("FormatCost(%f) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
