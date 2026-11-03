package provider

import (
	"testing"
)

func TestFormatModelName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-sonnet-4-5-20250929", "claude-sonnet-4-5"},
		{"claude-3-5-sonnet-20241022", "claude-3-5-sonnet"},
		{"claude-opus-4-6-20251001", "claude-opus-4-6"},
		{"Gemini 3.8 Flash (Medium)", "gemini-3.8-flash"},
		{"Gemini 3.8 Flash (Low)", "gemini-3.8-flash"},
		{"Claude Sonnet 4.6 (Thinking)", "claude-sonnet-4.6"},
		{"gpt-5.5", "gpt-5.5"},
		{"glm-5-2", "glm-5-2"},
		{"qwen2.5-coder:7b", "qwen2.5-coder:7b"},
		{"<synthetic>", ""},
		{"None", ""},
		{"", ""},
		{`"claude-sonnet-4-5"`, "claude-sonnet-4-5"},
	}

	for _, tc := range tests {
		got := FormatModelName(tc.input)
		if got != tc.expected {
			t.Errorf("FormatModelName(%q): expected %q, got %q", tc.input, tc.expected, got)
		}
	}
}
