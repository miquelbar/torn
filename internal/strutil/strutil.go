package strutil

import (
	"strings"
	"unicode/utf8"
)

// Truncate safely cuts a string to at most maxRunes without splitting multi-byte UTF-8 runes.
// If the string exceeds maxRunes, it is truncated on a rune boundary and suffix is appended.
func Truncate(s string, maxRunes int, suffix string) string {
	if maxRunes <= 0 {
		return suffix
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}

	count := 0
	byteIdx := len(s)
	for i := range s {
		if count == maxRunes {
			byteIdx = i
			break
		}
		count++
	}

	return strings.TrimRight(s[:byteIdx], " ") + suffix
}

// SafeSnippet extracts a substring around byte offsets [start, end] in s,
// adjusting boundaries so they never bisect multi-byte UTF-8 sequences.
func SafeSnippet(s string, startByte int, endByte int) string {
	if s == "" {
		return ""
	}
	if startByte < 0 {
		startByte = 0
	}
	if endByte > len(s) {
		endByte = len(s)
	}
	if startByte >= endByte {
		return ""
	}

	// Adjust startByte forward until it points to the start of a rune
	for startByte < endByte && !utf8.RuneStart(s[startByte]) {
		startByte++
	}

	// Adjust endByte forward until it points to the start of the next rune (or EOF)
	for endByte < len(s) && !utf8.RuneStart(s[endByte]) {
		endByte++
	}

	if startByte >= endByte {
		return ""
	}

	return s[startByte:endByte]
}
