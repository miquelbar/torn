package stats

import (
	"testing"
	"time"

	"torn/internal/cooldown"
)

func TestStatsCacheSaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()

	testStats := []ProviderStats{
		{
			Name:         "claude",
			Status:       cooldown.StatusReady,
			TotalTokens:  123456,
			LastModel:    "claude-sonnet-4-5",
			TokensPerSec: 55.5,
		},
	}

	// 1. Should be empty initially
	if _, ok := LoadCache(tempDir, time.Minute); ok {
		t.Fatal("expected empty cache on first load")
	}

	// 2. Save
	if err := SaveCache(tempDir, testStats); err != nil {
		t.Fatalf("SaveCache failed: %v", err)
	}

	// 3. Load valid cache
	loaded, ok := LoadCache(tempDir, time.Minute)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if len(loaded) != 1 || loaded[0].Name != "claude" || loaded[0].TotalTokens != 123456 {
		t.Errorf("unexpected loaded stats: %+v", loaded)
	}

	// 4. Test expired cache (maxAge = 0)
	time.Sleep(10 * time.Millisecond)
	if _, ok := LoadCache(tempDir, 1*time.Millisecond); ok {
		t.Fatal("expected cache miss for expired cache")
	}
}
