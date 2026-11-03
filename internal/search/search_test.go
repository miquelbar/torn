package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"torn/internal/provider"
)

func TestEngineFind(t *testing.T) {
	tempDir := t.TempDir()
	claude := provider.NewClaude(tempDir)
	reg := provider.NewRegistry(claude)
	engine := NewEngine(reg)

	cwd := "/Users/test/workspace"
	slug := claude.PathToSlug(cwd)
	projectDir := filepath.Join(tempDir, "projects", slug)
	_ = os.MkdirAll(projectDir, 0755)

	content := `{"role": "user", "content": "How do we fix the database deadlock issue?"}
{"role": "assistant", "content": "We can reorder transactions to prevent deadlock."}
`
	_ = os.WriteFile(filepath.Join(projectDir, "sess-100.jsonl"), []byte(content), 0644)

	results, err := engine.Find(cwd, "deadlock")
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Provider != "claude" {
		t.Errorf("expected claude provider, got %s", results[0].Provider)
	}
	if results[0].SessionID != "sess-100" {
		t.Errorf("expected session sess-100, got %s", results[0].SessionID)
	}
}

func TestEngineFindMultibyteSnippet(t *testing.T) {
	tempDir := t.TempDir()
	claude := provider.NewClaude(tempDir)
	reg := provider.NewRegistry(claude)
	engine := NewEngine(reg)

	cwd := "/Users/test/workspace"
	slug := claude.PathToSlug(cwd)
	projectDir := filepath.Join(tempDir, "projects", slug)
	_ = os.MkdirAll(projectDir, 0755)

	content := `{"role": "user", "content": "El niño comió piña en España 🇪🇸 mientras investigaba el error de compilación"}
{"role": "assistant", "content": "Solucionado el problema en España 🇪🇸 con éxito."}
`
	_ = os.WriteFile(filepath.Join(projectDir, "sess-utf8.jsonl"), []byte(content), 0644)

	results, err := engine.Find(cwd, "piña")
	if err != nil {
		t.Fatalf("Find failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !strings.Contains(results[0].Snippet, "piña") {
		t.Errorf("expected snippet to contain 'piña', got %q", results[0].Snippet)
	}
}

func TestEngineEmptyQuery(t *testing.T) {
	reg := provider.NewRegistry()
	engine := NewEngine(reg)

	_, err := engine.Find("/some/path", "   ")
	if err == nil {
		t.Fatal("expected error on empty query, got nil")
	}
}
