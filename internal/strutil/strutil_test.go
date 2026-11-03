package strutil

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateASCII(t *testing.T) {
	s := "hello world"
	res := Truncate(s, 5, "...")
	if res != "hello..." {
		t.Errorf("expected 'hello...', got %q", res)
	}
	if !utf8.ValidString(res) {
		t.Errorf("expected valid UTF-8 string, got invalid")
	}

	if Truncate(s, 20, "...") != "hello world" {
		t.Errorf("expected unmodified string when within limit")
	}

	if Truncate(s, 0, "...") != "..." {
		t.Errorf("expected suffix when maxRunes <= 0")
	}
}

func TestTruncateMultibyteUTF8(t *testing.T) {
	// "El niño comió piña en España 🇪🇸"
	s := "El niño comió piña en España 🇪🇸"

	for limit := 1; limit <= utf8.RuneCountInString(s); limit++ {
		res := Truncate(s, limit, "... [truncated]")
		if !utf8.ValidString(res) {
			t.Fatalf("Truncate(limit=%d) generated invalid UTF-8: %q", limit, res)
		}
	}

	// Specifically test slicing right through multi-byte character ñ (2 bytes)
	// "niñ" has runes 'n', 'i', 'ñ' (4 bytes)
	prefix := Truncate("niño", 3, "")
	if prefix != "niñ" {
		t.Errorf("expected 'niñ', got %q", prefix)
	}
	if !utf8.ValidString(prefix) {
		t.Errorf("expected valid UTF-8 for 'niñ'")
	}

	// Test emojis (4 bytes each)
	emojis := "🟢🟡🔴🚀"
	twoEmojis := Truncate(emojis, 2, "")
	if twoEmojis != "🟢🟡" {
		t.Errorf("expected '🟢🟡', got %q", twoEmojis)
	}
	if !utf8.ValidString(twoEmojis) {
		t.Errorf("expected valid UTF-8 for emojis")
	}
}

func TestSafeSnippet(t *testing.T) {
	s := "Error: fallo en niño 🇪🇸 al compilar"

	// Deliberately choose byte offsets that fall inside the multi-byte rune 'ñ'
	// 'ñ' is at byte index 20 (bytes 20 and 21)
	idx := 20
	// If startByte falls at byte 21 (continuation byte), it should snap to byte 22
	snippet := SafeSnippet(s, idx+1, idx+10)
	if !utf8.ValidString(snippet) {
		t.Fatalf("SafeSnippet generated invalid UTF-8: %q", snippet)
	}

	// Out of bounds checks
	if SafeSnippet("", 0, 10) != "" {
		t.Errorf("expected empty string for empty input")
	}
	if SafeSnippet(s, 50, 60) != "" {
		t.Errorf("expected empty string for out of bound range")
	}
}

func TestTruncatePath(t *testing.T) {
	ws := "/Users/test/Projects/my-app"

	// 1. Path inside workspace becomes relative
	p1 := "/Users/test/Projects/my-app/apps/api/internal/portada/worker.go"
	res1 := TruncatePath(p1, ws, 60)
	if res1 != "apps/api/internal/portada/worker.go" {
		t.Errorf("expected relative path 'apps/api/internal/portada/worker.go', got %q", res1)
	}

	// 2. Relative path that exceeds maxRunes truncates directory prefix preserving filename
	p2 := "apps/frontend/features/admin/dashboard/analytics/charts/performance/summary_view_controller.tsx"
	res2 := TruncatePath(p2, ws, 45)
	expectedBase := "summary_view_controller.tsx"
	if !strings.HasSuffix(res2, expectedBase) {
		t.Errorf("expected suffix %q, got %q", expectedBase, res2)
	}
	if !strings.HasPrefix(res2, ".../") {
		t.Errorf("expected prefix '.../', got %q", res2)
	}

	// 3. Absolute path outside workspace truncates prefix preserving filename
	p3 := "/Users/test/.gemini/antigravity-cli/brain/eb96e783-3cb9-4048-9125-ab28695a7575/task.log"
	res3 := TruncatePath(p3, ws, 35)
	if !strings.HasSuffix(res3, "task.log") {
		t.Errorf("expected suffix 'task.log', got %q", res3)
	}
	if !strings.HasPrefix(res3, ".../") {
		t.Errorf("expected prefix '.../', got %q", res3)
	}

	// 4. Empty and edge cases
	if TruncatePath("", ws, 20) != "" {
		t.Errorf("expected empty string for empty path")
	}
	if TruncatePath("/a/b/c.go", "", 0) != "c.go" {
		t.Errorf("expected 'c.go' when maxRunes <= 0")
	}
}
