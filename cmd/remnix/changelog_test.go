package main

import (
	"testing"
)

func TestChangelogHelp(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"changelog", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}
