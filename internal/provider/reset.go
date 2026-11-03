package provider

import (
	"regexp"
	"strings"
	"time"
)

var resetRegex = regexp.MustCompile(`(?i)resets\s+(\d{1,2}(?::\d{2})?[ap]m)(?:\s*\(([^)]+)\))?`)

// ExtractResetTime extracts cooldown reset timestamps from messages like:
// "You've hit your session limit · resets 12:30pm (Europe/Madrid) · progress saved"
func ExtractResetTime(text string, refTime time.Time, now time.Time) *time.Time {
	m := resetRegex.FindStringSubmatch(text)
	if len(m) < 2 {
		return nil
	}

	timeStr := strings.ToLower(strings.TrimSpace(m[1]))
	var parsedHour, parsedMin int
	parsed, err := time.Parse("3:04pm", timeStr)
	if err != nil {
		parsed, err = time.Parse("3pm", timeStr)
	}
	if err != nil {
		return nil
	}
	parsedHour = parsed.Hour()
	parsedMin = parsed.Minute()

	loc := now.Location()
	if len(m) > 2 && m[2] != "" {
		if l, err := time.LoadLocation(strings.TrimSpace(m[2])); err == nil {
			loc = l
		}
	}

	if refTime.IsZero() {
		refTime = now
	}
	refInLoc := refTime.In(loc)
	candidate := time.Date(refInLoc.Year(), refInLoc.Month(), refInLoc.Day(), parsedHour, parsedMin, 0, 0, loc)

	if candidate.Before(refInLoc) {
		candidate = candidate.AddDate(0, 0, 1)
	}

	if candidate.After(now) {
		return &candidate
	}

	return nil
}
