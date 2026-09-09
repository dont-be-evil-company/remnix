package tui

import (
	"bytes"
	"strings"
	"testing"
)

func TestStatusSetPrintsWhenNotTTY(t *testing.T) {
	var buf bytes.Buffer
	s := StartStatus(&buf)
	s.Set("pulling events")
	s.Set("pulling events")
	s.Set("writing checkpoint")
	s.Finish(nil)
	got := buf.String()
	if !strings.Contains(got, "pulling events") || !strings.Contains(got, "writing checkpoint") {
		t.Fatalf("output %q", got)
	}
	if strings.Count(got, "pulling events") != 1 {
		t.Fatalf("duplicate stages: %q", got)
	}
	if !strings.Contains(got, "synced") {
		t.Fatalf("missing done line: %q", got)
	}
}
