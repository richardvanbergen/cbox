package main

import "testing"

func TestChatCmd_OpenFlagNoOptDefVal(t *testing.T) {
	cmd := chatCmd()
	f := cmd.Flags().Lookup("open")
	if f == nil {
		t.Fatal("expected --open flag to be registered")
	}
	if f.NoOptDefVal != " " {
		t.Errorf("NoOptDefVal = %q, want %q", f.NoOptDefVal, " ")
	}
}

func TestChatCmd_OpenFlagNotChangedByDefault(t *testing.T) {
	cmd := chatCmd()
	// Simulate parsing with no --open flag.
	if err := cmd.ParseFlags([]string{"mybranch"}); err != nil {
		t.Fatal(err)
	}
	if cmd.Flags().Changed("open") {
		t.Error("expected --open to not be changed when not provided")
	}
}

func TestChatCmd_OpenFlagChangedWhenProvided(t *testing.T) {
	cmd := chatCmd()
	// Simulate parsing with --open flag (no value, uses NoOptDefVal).
	if err := cmd.ParseFlags([]string{"--open", "mybranch"}); err != nil {
		t.Fatal(err)
	}
	if !cmd.Flags().Changed("open") {
		t.Error("expected --open to be changed when explicitly provided")
	}
}

func TestChatCmd_OpenFlagChangedWithValue(t *testing.T) {
	cmd := chatCmd()
	// Simulate parsing with --open=command.
	if err := cmd.ParseFlags([]string{"--open=vim $Dir", "mybranch"}); err != nil {
		t.Fatal(err)
	}
	if !cmd.Flags().Changed("open") {
		t.Error("expected --open to be changed when provided with value")
	}
	val, err := cmd.Flags().GetString("open")
	if err != nil {
		t.Fatal(err)
	}
	if val != "vim $Dir" {
		t.Errorf("open flag value = %q, want %q", val, "vim $Dir")
	}
}
