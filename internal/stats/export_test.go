package stats

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"torn/internal/cooldown"
	"torn/internal/provider"
)

func sampleStatsList() []ProviderStats {
	return []ProviderStats{
		{
			Name:          "claude",
			Status:        cooldown.StatusActive,
			SessionsCount: 4,
			LastModel:     "claude-sonnet-4-5",
			UniqueModels:  []string{"claude-opus-4-6", "claude-sonnet-4-5"},
			TokensToday:   15400,
			TotalTokens:   1250000,
			TokensPerSec:  48.5,
			ModelStats: map[string]provider.ModelMetric{
				"claude-opus-4-6": {
					Model:        "claude-opus-4-6",
					Turns:        15,
					TokensToday:  3400,
					TotalTokens:  250000,
					TokensPerSec: 32.1,
				},
				"claude-sonnet-4-5": {
					Model:        "claude-sonnet-4-5",
					Turns:        50,
					TokensToday:  12000,
					TotalTokens:  1000000,
					TokensPerSec: 55.4,
				},
			},
		},
		{
			Name:          "codex",
			Status:        cooldown.StatusCooldown,
			CooldownTime:  35 * time.Minute,
			SessionsCount: 2,
			LastModel:     "gpt-5",
			UniqueModels:  []string{"gpt-5"},
			TokensToday:   0,
			TotalTokens:   450000,
			TokensPerSec:  0,
		},
		{
			Name:          "vibe",
			Status:        cooldown.StatusReady,
			SessionsCount: 0,
			TokensToday:   0,
			TotalTokens:   0,
		},
	}
}

func TestRenderMarkdown(t *testing.T) {
	statsList := sampleStatsList()
	md := RenderMarkdown(statsList)

	if !strings.Contains(md, "# Torn Hypervisor Stats") {
		t.Fatalf("expected title in markdown output, got:\n%s", md)
	}

	// Verify table header
	if !strings.Contains(md, "| Provider | Status | Cooldown | Sessions | Model | Today | Total | Speed |") {
		t.Errorf("missing main table header in markdown, got:\n%s", md)
	}
	if !strings.Contains(md, "| :--- | :--- | :--- | ---: | :--- | ---: | ---: | ---: |") {
		t.Errorf("missing table separator in markdown, got:\n%s", md)
	}

	// Verify provider rows
	if !strings.Contains(md, "| claude | ACTIVE | — | 4 | sonnet-4-5 (+1) | 15,400 | 1,250,000 | 48.5 t/s |") {
		t.Errorf("claude row formatted incorrectly, got:\n%s", md)
	}
	if !strings.Contains(md, "| codex | COOLDOWN | 35m | 2 | gpt-5 | 0 | 450,000 | — |") {
		t.Errorf("codex row formatted incorrectly, got:\n%s", md)
	}
	if !strings.Contains(md, "| vibe | READY | — | 0 | — | 0 | 0 | — |") {
		t.Errorf("vibe row formatted incorrectly, got:\n%s", md)
	}

	// Verify Model Breakdown
	if !strings.Contains(md, "## Model Breakdown") {
		t.Errorf("missing model breakdown header, got:\n%s", md)
	}
	if !strings.Contains(md, "| claude | sonnet-4-5 | 50 | 12,000 | 1,000,000 | 55.4 t/s |") {
		t.Errorf("sonnet-4-5 breakdown row incorrect, got:\n%s", md)
	}
	if !strings.Contains(md, "| claude | opus-4-6 | 15 | 3,400 | 250,000 | 32.1 t/s |") {
		t.Errorf("opus-4-6 breakdown row incorrect, got:\n%s", md)
	}
}

func TestRenderCSV(t *testing.T) {
	statsList := sampleStatsList()
	rawCSV, err := RenderCSV(statsList)
	if err != nil {
		t.Fatalf("RenderCSV returned error: %v", err)
	}

	sections := strings.Split(strings.TrimSpace(rawCSV), "\n\n")
	if len(sections) != 2 {
		t.Fatalf("expected 2 CSV sections (providers and models), got %d sections:\n%s", len(sections), rawCSV)
	}

	// Test section 1: Providers
	r1 := csv.NewReader(strings.NewReader(sections[0]))
	records1, err := r1.ReadAll()
	if err != nil {
		t.Fatalf("failed reading provider CSV section: %v", err)
	}
	if len(records1) != 4 { // 1 header + 3 providers
		t.Fatalf("expected 4 rows in provider CSV section, got %d", len(records1))
	}
	if records1[0][0] != "Provider" || records1[0][4] != "Model" {
		t.Errorf("unexpected provider header: %v", records1[0])
	}
	if records1[1][0] != "claude" || records1[1][1] != "ACTIVE" || records1[1][5] != "15400" {
		t.Errorf("unexpected claude CSV row: %v", records1[1])
	}

	// Test section 2: Model Breakdown
	r2 := csv.NewReader(strings.NewReader(sections[1]))
	records2, err := r2.ReadAll()
	if err != nil {
		t.Fatalf("failed reading model CSV section: %v", err)
	}
	if len(records2) != 3 { // 1 header + 2 models
		t.Fatalf("expected 3 rows in model CSV section, got %d", len(records2))
	}
	if records2[0][0] != "Provider" || records2[0][1] != "Model" {
		t.Errorf("unexpected model header: %v", records2[0])
	}
}

func TestRenderJSON(t *testing.T) {
	statsList := sampleStatsList()
	jsonStr, err := RenderJSON(statsList)
	if err != nil {
		t.Fatalf("RenderJSON failed: %v", err)
	}

	var parsed []ProviderStats
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		t.Fatalf("unmarshaling rendered JSON failed: %v\nJSON:\n%s", err, jsonStr)
	}
	if len(parsed) != 3 {
		t.Fatalf("expected 3 providers in parsed JSON, got %d", len(parsed))
	}
	if parsed[0].Name != "claude" || parsed[0].TokensToday != 15400 {
		t.Errorf("unexpected parsed data for claude: %+v", parsed[0])
	}
}

func TestRenderFormatDispatch(t *testing.T) {
	statsList := sampleStatsList()

	// Default / table
	outTable, err := Render(statsList, "table")
	if err != nil || !strings.Contains(outTable, "TORN HYPERVISOR STATS") {
		t.Errorf("expected table output, got error: %v, out: %s", err, outTable)
	}

	// Markdown aliases
	outMD, err := Render(statsList, "markdown")
	if err != nil || !strings.Contains(outMD, "# Torn Hypervisor Stats") {
		t.Errorf("expected markdown output, got error: %v, out: %s", err, outMD)
	}
	outMD2, err := Render(statsList, "md")
	if err != nil || outMD2 != outMD {
		t.Errorf("expected md alias to match markdown")
	}

	// CSV
	outCSV, err := Render(statsList, "csv")
	if err != nil || !strings.Contains(outCSV, "Provider,Status,Cooldown") {
		t.Errorf("expected CSV output, got error: %v, out: %s", err, outCSV)
	}

	// JSON
	outJSON, err := Render(statsList, "json")
	if err != nil || !strings.Contains(outJSON, `"last_model": "claude-sonnet-4-5"`) {
		t.Errorf("expected JSON output, got error: %v, out: %s", err, outJSON)
	}

	// Unsupported format
	_, err = Render(statsList, "xml")
	if err == nil {
		t.Errorf("expected error for unsupported format 'xml'")
	}
}
