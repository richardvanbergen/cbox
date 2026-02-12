package main

import (
	"testing"
)

func TestOpenCmd_RequiresBranchArg(t *testing.T) {
	cmd := openCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when no branch arg provided")
	}
}

func TestOpenCmd_RejectsExtraArgs(t *testing.T) {
	cmd := openCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{"branch1", "branch2"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when extra args provided")
	}
}

func TestOpenCmd_HasOpenFlag(t *testing.T) {
	cmd := openCmd()
	f := cmd.Flags().Lookup("open")
	if f == nil {
		t.Fatal("expected --open flag to be defined")
	}
	if f.DefValue != "" {
		t.Errorf("expected --open default to be empty, got %q", f.DefValue)
	}
}
