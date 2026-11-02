package search

import (
	"fmt"
	"strings"
	"time"

	"torn/internal/provider"
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

	var results []MatchResult

	for _, p := range e.registry.All() {
		sess, err := p.DetectLatestSession(cwd)
		if err != nil || sess == nil {
			continue
		}

		details, err := p.ParseSession(sess.Path)
		if err != nil || details == nil {
			continue
		}

		for _, turn := range details.Turns {
			turnLower := strings.ToLower(turn.Text)
			if idx := strings.Index(turnLower, cleanQuery); idx != -1 {
				// Extract snippet around match
				start := idx - 40
				if start < 0 {
					start = 0
				}
				end := idx + len(cleanQuery) + 60
				if end > len(turn.Text) {
					end = len(turn.Text)
				}

				snippet := strings.ReplaceAll(turn.Text[start:end], "\n", " ")
				if start > 0 {
					snippet = "..." + snippet
				}
				if end < len(turn.Text) {
					snippet = snippet + "..."
				}

				results = append(results, MatchResult{
					Provider:  p.Name(),
					SessionID: sess.ID,
					Timestamp: sess.UpdatedAt,
					Turns:     len(details.Turns),
					Snippet:   strings.TrimSpace(snippet),
				})
				break
			}
		}
	}

	return results, nil
}
