package clipboard

import (
	"testing"
)

func TestClipboardMockWriter(t *testing.T) {
	var captured string
	mockWriter := func(text string) error {
		captured = text
		return nil
	}

	testText := "standardized handoff prompt"
	if err := mockWriter(testText); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if captured != testText {
		t.Errorf("expected %q, got %q", testText, captured)
	}
}
