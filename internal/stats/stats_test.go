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
