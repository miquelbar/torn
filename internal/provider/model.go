package provider

import (
	"regexp"
	"strings"
)

var (
	dateSuffixRegex = regexp.MustCompile(`-\d{8}$`)
)

// FormatModelName normalizes raw model identifiers from CLI logs:
// - Strips synthetic values like "<synthetic>" or "None"
// - Strips trailing snapshot dates (e.g. "claude-sonnet-4-5-20250929" -> "claude-sonnet-4-5")
// - Strips parenthetical modes (e.g. "Gemini 3.8 Flash (Medium)" -> "gemini-3.8-flash")
// - Normalizes spaces to hyphens and lowercase
func FormatModelName(raw string) string {
	s := strings.Trim(strings.TrimSpace(raw), `"'`)
	if s == "" || s == "<synthetic>" || strings.EqualFold(s, "none") {
		return ""
	}

	// Strip parentheses e.g. "Gemini 3.8 Flash (Medium)" -> "Gemini 3.8 Flash"
	if idx := strings.Index(s, "("); idx != -1 {
		s = strings.TrimSpace(s[:idx])
	}

	// Lowercase and convert spaces to hyphens
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")

	// Strip trailing date suffix (e.g. "-20250929" or "-20241022")
	s = dateSuffixRegex.ReplaceAllString(s, "")

	return s
}
