package main

import (
	"bytes"
	"testing"
)

func TestResolveVersion_Fallback(t *testing.T) {
	// When version is not set via ldflags, resolveVersion should return
	// either a git short hash or "dev" — never an empty string.
	v := resolveVersion()
	if v == "" {
		t.Fatal("resolveVersion() returned empty string")
	}
}

func TestResolveVersion_LdflagsOverride(t *testing.T) {
	// When version is set via ldflags, it should be returned as-is.
	old := version
	version = "v1.2.3-test"
	defer func() { version = old }()

	v := resolveVersion()
	if v != "v1.2.3-test" {
		t.Errorf("resolveVersion() = %q, want %q", v, "v1.2.3-test")
	}
}

func TestRootCmd_VersionFlag(t *testing.T) {
	old := version
	version = "v0.0.1-test"
	defer func() { version = old }()

	root := buildRootCmd()

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"--version"})

	if err := root.Execute(); err != nil {
		t.Fatalf("--version returned error: %v", err)
	}

	got := buf.String()
	if got == "" {
		t.Fatal("--version produced no output")
	}
	// cobra outputs "cbox version v0.0.1-test\n"
	want := "cbox version v0.0.1-test\n"
	if got != want {
		t.Errorf("--version output = %q, want %q", got, want)
	}
}
