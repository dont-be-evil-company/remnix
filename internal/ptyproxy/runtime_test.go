//go:build unix

package ptyproxy

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

func TestSnapshotFromEmulator(t *testing.T) {
	emu := vt.NewEmulator(10, 3)
	emu.SetScrollbackSize(0)
	_, _ = emu.Write([]byte("one\r\ntwo"))
	s := snapshotFrom(emu)
	if s.Rows != 3 || s.Cols != 10 {
		t.Fatalf("size %d x %d", s.Rows, s.Cols)
	}
	if s.CursorRow != 1 {
		t.Fatalf("cursor row %d", s.CursorRow)
	}
	joined := strings.Join(s.RowANSI, "\n")
	if !strings.Contains(joined, "one") || !strings.Contains(joined, "two") {
		t.Fatalf("rows: %q", joined)
	}
	got, err := Decode(Encode(s))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.RowANSI[0], "one") {
		t.Fatalf("round-trip: %#v", got.RowANSI)
	}
}
