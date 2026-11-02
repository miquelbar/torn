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
	if !strings.Contains(output, "🟡 EN USO") {
		t.Errorf("output missing in-use status for claude")
	}
	if !strings.Contains(output, "🔴 BLOQUEADO") {
		t.Errorf("output missing blocked status for codex")
	}
	if !strings.Contains(output, "🟢 LIBRE") {
		t.Errorf("output missing free status for other providers")
	}
}
