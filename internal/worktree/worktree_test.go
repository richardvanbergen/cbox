package worktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyFiles_SingleFile(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create a source file.
	if err := os.WriteFile(filepath.Join(src, ".env"), []byte("SECRET=abc"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFiles(src, dst, []string{".env"}); err != nil {
		t.Fatalf("CopyFiles: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, ".env"))
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(got) != "SECRET=abc" {
		t.Errorf("got %q, want %q", string(got), "SECRET=abc")
	}
}

func TestCopyFiles_Directory(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create a source directory with nested files.
	dir := filepath.Join(src, "config", "sub")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "config", "a.txt"), []byte("aaa"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bbb"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFiles(src, dst, []string{"config"}); err != nil {
		t.Fatalf("CopyFiles: %v", err)
	}

	// Verify both files were copied.
	gotA, err := os.ReadFile(filepath.Join(dst, "config", "a.txt"))
	if err != nil {
		t.Fatalf("reading config/a.txt: %v", err)
	}
	if string(gotA) != "aaa" {
		t.Errorf("config/a.txt: got %q, want %q", string(gotA), "aaa")
	}

	gotB, err := os.ReadFile(filepath.Join(dst, "config", "sub", "b.txt"))
	if err != nil {
		t.Fatalf("reading config/sub/b.txt: %v", err)
	}
	if string(gotB) != "bbb" {
		t.Errorf("config/sub/b.txt: got %q, want %q", string(gotB), "bbb")
	}
}

func TestCopyFiles_MissingSourceSkipped(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// No source file exists — should not error.
	if err := CopyFiles(src, dst, []string{".env", "missing.txt"}); err != nil {
		t.Fatalf("CopyFiles should skip missing files, got: %v", err)
	}

	// Nothing should have been created.
	entries, _ := os.ReadDir(dst)
	if len(entries) != 0 {
		t.Errorf("expected empty destination, got %d entries", len(entries))
	}
}

func TestCopyFiles_PreservesPermissions(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	srcPath := filepath.Join(src, "script.sh")
	if err := os.WriteFile(srcPath, []byte("#!/bin/sh"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := CopyFiles(src, dst, []string{"script.sh"}); err != nil {
		t.Fatalf("CopyFiles: %v", err)
	}

	info, err := os.Stat(filepath.Join(dst, "script.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("permissions: got %o, want %o", info.Mode().Perm(), 0755)
	}
}

func TestCopyFiles_NestedFilePath(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create a file in a subdirectory.
	if err := os.MkdirAll(filepath.Join(src, "deep"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "deep", "secret.key"), []byte("key123"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := CopyFiles(src, dst, []string{"deep/secret.key"}); err != nil {
		t.Fatalf("CopyFiles: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "deep", "secret.key"))
	if err != nil {
		t.Fatalf("reading copied file: %v", err)
	}
	if string(got) != "key123" {
		t.Errorf("got %q, want %q", string(got), "key123")
	}
}

func TestCopyFiles_MultiplePatterns(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.WriteFile(filepath.Join(src, ".env"), []byte("A=1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".env.local"), []byte("B=2"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFiles(src, dst, []string{".env", ".env.local"}); err != nil {
		t.Fatalf("CopyFiles: %v", err)
	}

	got1, err := os.ReadFile(filepath.Join(dst, ".env"))
	if err != nil {
		t.Fatalf("reading .env: %v", err)
	}
	if string(got1) != "A=1" {
		t.Errorf(".env: got %q, want %q", string(got1), "A=1")
	}

	got2, err := os.ReadFile(filepath.Join(dst, ".env.local"))
	if err != nil {
		t.Fatalf("reading .env.local: %v", err)
	}
	if string(got2) != "B=2" {
		t.Errorf(".env.local: got %q, want %q", string(got2), "B=2")
	}
}

func TestCopyFiles_EmptyList(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := CopyFiles(src, dst, nil); err != nil {
		t.Fatalf("CopyFiles with nil: %v", err)
	}
	if err := CopyFiles(src, dst, []string{}); err != nil {
		t.Fatalf("CopyFiles with empty: %v", err)
	}
}
