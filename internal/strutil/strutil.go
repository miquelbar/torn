package strutil

import (
	"path/filepath"
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

// TruncatePath formats a file path for high-density CLI display:
//  1. If path is inside workspace, it converts path to a relative path.
//  2. If the resulting path exceeds maxRunes, it truncates the directory prefix
//     while preserving filepath.Base(path) intact at the end (e.g. ".../internal/portada/worker.go").
func TruncatePath(path string, workspace string, maxRunes int) string {
	cleanVal := strings.Trim(strings.TrimSpace(path), "\"'")
	if cleanVal == "" {
		return ""
	}
	cleanPath := filepath.Clean(cleanVal)

	// 1. If workspace is provided, make relative if path is within workspace
	if workspace != "" {
		cleanWS := filepath.Clean(workspace)
		if rel, err := filepath.Rel(cleanWS, cleanPath); err == nil && !strings.HasPrefix(rel, "..") {
			cleanPath = rel
		}
	}

	if maxRunes <= 0 {
		return filepath.Base(cleanPath)
	}

	if utf8.RuneCountInString(cleanPath) <= maxRunes {
		return cleanPath
	}

	base := filepath.Base(cleanPath)
	baseRunes := utf8.RuneCountInString(base)

	// If filename alone exceeds or equals maxRunes, truncate filename preserving extension
	if baseRunes >= maxRunes {
		ext := filepath.Ext(base)
		if len(ext) > 0 && len(ext) < maxRunes-4 {
			rem := maxRunes - len(ext) - 3
			return Truncate(base[:len(base)-len(ext)], rem, "...") + ext
		}
		return Truncate(base, maxRunes, "...")
	}

	// We want ".../<tail-of-dir>/<base>"
	// Prefix ellipsis is ".../" (4 runes)
	remDirRunes := maxRunes - baseRunes - 4
	if remDirRunes <= 0 {
		return ".../" + base
	}

	dir := filepath.Dir(cleanPath)
	dirRunes := []rune(dir)
	if len(dirRunes) > remDirRunes {
		tail := string(dirRunes[len(dirRunes)-remDirRunes:])
		if slashIdx := strings.Index(tail, string(filepath.Separator)); slashIdx != -1 && slashIdx < len(tail)-1 {
			tail = tail[slashIdx+1:]
		}
		return ".../" + filepath.Join(tail, base)
	}

	return cleanPath
}
