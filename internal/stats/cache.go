package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// StatsCache records serialized ProviderStats with generation timestamp.
type StatsCache struct {
	Timestamp time.Time       `json:"timestamp"`
	Stats     []ProviderStats `json:"stats"`
}

// LoadCache returns cached stats if present and younger than maxAge.
func LoadCache(cacheDir string, maxAge time.Duration) ([]ProviderStats, bool) {
	cachePath := filepath.Join(cacheDir, "stats_cache.json")
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, false
	}

	var c StatsCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, false
	}

	if time.Since(c.Timestamp) > maxAge {
		return nil, false
	}

	return c.Stats, true
}

// SaveCache writes stats to cacheDir atomically.
func SaveCache(cacheDir string, stats []ProviderStats) error {
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return err
	}

	c := StatsCache{
		Timestamp: time.Now(),
		Stats:     stats,
	}

	data, err := json.Marshal(c)
	if err != nil {
		return err
	}

	cachePath := filepath.Join(cacheDir, "stats_cache.json")
	tmpFile, err := os.CreateTemp(cacheDir, "stats_cache.*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer os.Remove(tmpName)

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, cachePath)
}
