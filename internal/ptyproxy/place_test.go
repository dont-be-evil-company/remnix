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
