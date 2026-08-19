package main

import (
	"testing"
)

func TestRootHelp(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestUnlockAndDaemonHelp(t *testing.T) {
	for _, args := range [][]string{{"unlock", "--help"}, {"daemon", "--help"}, {"search", "--help"}, {"config", "--help"}, {"remote", "--help"}, {"version", "--help"}} {
		cmd := newRootCmd()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
}
