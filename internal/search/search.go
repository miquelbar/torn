package search

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"torn/internal/provider"
	"torn/internal/strutil"
)

// MatchResult represents a matched session containing the query term.
type MatchResult struct {
	Provider  string    `json:"provider"`
	SessionID string    `json:"session_id"`
	Timestamp time.Time `json:"timestamp"`
	Turns     int       `json:"turns"`
	Snippet   string    `json:"snippet"`
}

// Engine performs session search across AI CLI providers.
type Engine struct {
	registry *provider.Registry
}

// NewEngine creates a new search Engine.
func NewEngine(reg *provider.Registry) *Engine {
	return &Engine{registry: reg}
}

// Find searches for query in sessions belonging to cwd across all providers.
func (e *Engine) Find(cwd string, query string) ([]MatchResult, error) {
	cleanQuery := strings.ToLower(strings.TrimSpace(query))
	if cleanQuery == "" {
		return nil, fmt.Errorf("search query cannot be empty")
	}

	providers := e.registry.All()
	var (
		mu      sync.Mutex
		results []MatchResult
		wg      sync.WaitGroup
	)

	for _, p := range providers {
		wg.Add(1)
		go func(prov provider.Provider) {
			defer wg.Done()

			sess, err := prov.DetectLatestSession(cwd)
			if err != nil || sess == nil {
				return
			}

			details, err := prov.ParseSession(sess.Path)
			if err != nil || details == nil {
				return
			}

			for _, turn := range details.Turns {
				turnLower := strings.ToLower(turn.Text)
				if idx := strings.Index(turnLower, cleanQuery); idx != -1 {
					// Extract snippet around match safely avoiding split runes
					start := idx - 40
					if start < 0 {
						start = 0
					}
					end := idx + len(cleanQuery) + 60
					if end > len(turn.Text) {
						end = len(turn.Text)
					}

					snippet := strutil.SafeSnippet(turn.Text, start, end)
					snippet = strings.ReplaceAll(snippet, "\n", " ")
					if start > 0 {
						snippet = "..." + snippet
					}
					if end < len(turn.Text) {
						snippet = snippet + "..."
					}

					res := MatchResult{
						Provider:  prov.Name(),
						SessionID: sess.ID,
						Timestamp: sess.UpdatedAt,
						Turns:     len(details.Turns),
						Snippet:   strings.TrimSpace(snippet),
					}

					mu.Lock()
					results = append(results, res)
					mu.Unlock()
					break
				}
			}
		}(p)
	}

	wg.Wait()
	return results, nil
}
