package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestChangelogHelp(t *testing.T) {
	cmd := newRootCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"changelog", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "latest|all") {
		t.Fatalf("help should require latest|all: %q", got)
	}
}

func TestChangelogRequiresArg(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"changelog"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error without arg")
	}
}

func TestChangelogLatestAndAll(t *testing.T) {
	for _, arg := range []string{"latest", "all"} {
		cmd := newRootCmd()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs([]string{"changelog", arg})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%s: %v", arg, err)
		}
		if out.Len() == 0 {
			t.Fatalf("%s: empty output", arg)
		}
	}
}

func TestChangelogRejectsVersion(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"changelog", "1.0.0"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for version selector")
	}
	if !strings.Contains(err.Error(), "latest or all") {
		t.Fatalf("error: %v", err)
	}
}
