package ptyproxy

import (
	"strings"
	"testing"
)

func TestPrepareCUPsToOverlayOrigin(t *testing.T) {
	s := Snapshot{Rows: 10, Cols: 8, CursorRow: 2, CursorCol: 1, RowANSI: make([]string, 10)}
	p := Place(2, 10, 8, 4)
	var b strings.Builder
	Prepare(&b, s, p)
	out := b.String()
	if p.Rect.Y != 2 {
		t.Fatalf("place Y %d", p.Rect.Y)
	}
	if !strings.Contains(out, "\x1b[3;1H") {
		t.Fatalf("missing overlay CUP: %q", out)
	}
	if strings.HasPrefix(out, "\x1b[1;1H") {
		t.Fatal("must not park at home; Bubble Tea inline paints from overlay origin")
	}
}

func TestContentCursorRowPinsBelowPrompt(t *testing.T) {
	s := Snapshot{
		Rows:      10,
		Cols:      8,
		CursorRow: 9,
		RowANSI:   []string{"prompt", "> remnix", "", "", "", "", "", "", "", ""},
	}
	if got := ContentCursorRow(s); got != 1 {
		t.Fatalf("got %d, want prompt row 1", got)
	}
	s.CursorRow = 1
	if got := ContentCursorRow(s); got != 1 {
		t.Fatalf("aligned cursor should stay %d", got)
	}
}

func TestContentCursorRowIgnoresSGRPadding(t *testing.T) {
	s := Snapshot{
		Rows:      4,
		Cols:      8,
		CursorRow: 3,
		RowANSI:   []string{"\x1b[0mhello", "\x1b[0m", "", "\x1b[0m"},
	}
	if got := ContentCursorRow(s); got != 0 {
		t.Fatalf("got %d", got)
	}
}
