package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHashPathDeterministic(t *testing.T) {
	p1 := "/Users/test/Projects/my-app"
	p2 := "/Users/test/Projects/my-app"
	p3 := "/Users/test/Projects/other-app"

	h1 := HashPath(p1)
	h2 := HashPath(p2)
	h3 := HashPath(p3)

	if h1 != h2 {
		t.Fatalf("expected identical hashes for same path, got %q and %q", h1, h2)
	}
	if len(h1) != 16 {
		t.Fatalf("expected hash length 16, got %d (%s)", len(h1), h1)
	}
	if h1 == h3 {
		t.Fatalf("expected different hashes for different paths, got collision: %s", h1)
	}
}

func TestDetectWithGit(t *testing.T) {
	tempDir := t.TempDir()

	mockGit := func(dir string, args ...string) (string, error) {
		if len(args) >= 2 && args[0] == "rev-parse" && args[1] == "HEAD" {
			return "abc1234567890", nil
		}
		return "", nil
	}

	info, err := DetectWithGit(tempDir, mockGit)
	if err != nil {
		t.Fatalf("DetectWithGit returned unexpected error: %v", err)
	}

	evalDir, _ := filepath.EvalSymlinks(tempDir)
	if info.Path != evalDir {
		t.Errorf("expected path %q, got %q", evalDir, info.Path)
	}
	if !info.IsGit {
		t.Errorf("expected IsGit to be true")
	}
	if info.GitHead != "abc1234567890" {
		t.Errorf("expected GitHead 'abc1234567890', got %q", info.GitHead)
	}
}

func TestDetectNonGit(t *testing.T) {
	tempDir := t.TempDir()

	mockGit := func(dir string, args ...string) (string, error) {
		return "", os.ErrNotExist
	}

	info, err := DetectWithGit(tempDir, mockGit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if info.IsGit {
		t.Errorf("expected IsGit to be false")
	}
	if info.GitHead != "" {
		t.Errorf("expected empty GitHead, got %q", info.GitHead)
	}
}

func TestDetectNonExistentDir(t *testing.T) {
	_, err := Detect("/non/existent/path/for/sure")
	if err == nil {
		t.Fatal("expected error for non-existent directory, got nil")
	}
}
