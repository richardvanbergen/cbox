package serve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraefikContainerName(t *testing.T) {
	name := TraefikContainerName("myapp")
	if name != "cbox-myapp-traefik" {
		t.Fatalf("expected cbox-myapp-traefik, got %s", name)
	}
}

func TestAddRoute(t *testing.T) {
	dir := t.TempDir()

	err := AddRoute(dir, "feature-auth", "myapp", 34567)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	path := filepath.Join(dir, ".cbox", "traefik", "dynamic", "feature-auth.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read route file: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, "feature-auth.myapp.dev.localhost") {
		t.Errorf("expected hostname in route, got:\n%s", content)
	}
	if !strings.Contains(content, "http://host.docker.internal:34567") {
		t.Errorf("expected backend URL in route, got:\n%s", content)
	}
}

func TestRemoveRoute(t *testing.T) {
	dir := t.TempDir()

	// Create a route first
	if err := AddRoute(dir, "feature-auth", "myapp", 34567); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Remove it
	if err := RemoveRoute(dir, "feature-auth"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's gone
	path := filepath.Join(dir, ".cbox", "traefik", "dynamic", "feature-auth.yml")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected route file to be removed")
	}
}

func TestRemoveRoute_NotExist(t *testing.T) {
	dir := t.TempDir()

	// Should not error on missing file
	if err := RemoveRoute(dir, "nonexistent"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHasRoutes(t *testing.T) {
	dir := t.TempDir()

	// No routes initially
	has, err := HasRoutes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("expected no routes")
	}

	// Add a route
	if err := AddRoute(dir, "feature-auth", "myapp", 34567); err != nil {
		t.Fatalf("setup: %v", err)
	}

	has, err = HasRoutes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !has {
		t.Fatal("expected routes to exist")
	}

	// Remove it
	if err := RemoveRoute(dir, "feature-auth"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	has, err = HasRoutes(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if has {
		t.Fatal("expected no routes after removal")
	}
}
