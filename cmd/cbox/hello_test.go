package main

import "testing"

func TestHelloCmd(t *testing.T) {
	cmd := helloCmd()
	if cmd.Use != "hello" {
		t.Errorf("Use = %q, want %q", cmd.Use, "hello")
	}
	if err := cmd.Execute(); err != nil {
		t.Fatalf("hello command returned error: %v", err)
	}
}
