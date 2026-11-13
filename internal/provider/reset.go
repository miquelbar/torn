package provider

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	resetRegex = regexp.MustCompile(`(?i)resets\s+(\d{1,2}(?::\d{2})?[ap]m)(?:\s*\(([^)]+)\))?`)
	// resetInRegex matches relative reset announcements like "Resets in 4h8m33s".
	resetInRegex = regexp.MustCompile(`(?i)reset(?:s)?\s+in\s+(?:(\d+)\s*h)?\s*(?:(\d+)\s*m)?\s*(?:(\d+)\s*s)?`)
	// quotaExhaustionRegex matches high-confidence quota exhaustion evidence.
	quotaExhaustionRegex = regexp.MustCompile(`(?i)(quota\s+reached|quota\s+exhausted|usage\s+limit|resource_exhausted|rate\s*limit\s+reached)`)
)

// ExtractResetTime extracts cooldown reset timestamps from messages like:
// "You've hit your session limit · resets 12:30pm (Europe/Madrid) · progress saved"
// or relative announcements like:
// "Individual quota reached. ... Resets in 4h8m33s."
//
// refTime anchors the calculation: the clock form resolves the nearest
// future occurrence of that time-of-day after refTime, while the relative
// form adds the announced duration to refTime. When refTime is zero, now is
// used as the anchor.
func ExtractResetTime(text string, refTime time.Time, now time.Time) *time.Time {
	if candidate := extractClockReset(text, refTime, now); candidate != nil {
		return candidate
	}
	return extractRelativeReset(text, refTime, now)
}

func extractClockReset(text string, refTime time.Time, now time.Time) *time.Time {
	m := resetRegex.FindStringSubmatch(text)
	if len(m) < 2 {
		return nil
	}

	timeStr := strings.ToLower(strings.TrimSpace(m[1]))
	parsed, err := time.Parse("3:04pm", timeStr)
	if err != nil {
		parsed, err = time.Parse("3pm", timeStr)
	}
	if err != nil {
		return nil
	}

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
	candidate := time.Date(refInLoc.Year(), refInLoc.Month(), refInLoc.Day(), parsed.Hour(), parsed.Minute(), 0, 0, loc)

	if candidate.Before(refInLoc) {
		candidate = candidate.AddDate(0, 0, 1)
	}

	if candidate.After(now) {
		return &candidate
	}

	return nil
}

func extractRelativeReset(text string, refTime time.Time, now time.Time) *time.Time {
	m := resetInRegex.FindStringSubmatch(text)
	if len(m) < 4 {
		return nil
	}
	if m[1] == "" && m[2] == "" && m[3] == "" {
		return nil
	}

	hours, _ := strconv.Atoi(m[1])
	mins, _ := strconv.Atoi(m[2])
	secs, _ := strconv.Atoi(m[3])
	duration := time.Duration(hours)*time.Hour + time.Duration(mins)*time.Minute + time.Duration(secs)*time.Second
	if duration <= 0 {
		return nil
	}

	if refTime.IsZero() {
		refTime = now
	}
	candidate := refTime.Add(duration)
	if candidate.After(now) {
		return &candidate
	}
	return nil
}

// DetectQuotaCooldown inspects an error or system message for quota
// exhaustion evidence. It returns the detected reset time only when the same
// message contains both an exhaustion marker (e.g. "quota reached",
// "RESOURCE_EXHAUSTED", "usage limit") and a reset announcement; plain
// conversation text that merely mentions quotas is ignored to prevent false
// positives.
func DetectQuotaCooldown(text string, msgTime time.Time, now time.Time) *time.Time {
	if !quotaExhaustionRegex.MatchString(text) {
		return nil
	}
	if !strings.Contains(strings.ToLower(text), "reset") {
		return nil
	}
	return ExtractResetTime(text, msgTime, now)
}

// FormatResetTime renders a cooldown reset instant as a compact local string
// with remaining time, e.g. "19:36 (2h 15m)".
func FormatResetTime(until time.Time, now time.Time) string {
	return fmt.Sprintf("%s (%s)", until.Format("15:04"), cooldownShort(until.Sub(now)))
}

func cooldownShort(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %02dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}
