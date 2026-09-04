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

func TestEncodeRowCoalescesAdjacentStyle(t *testing.T) {
	emu := vt.NewEmulator(8, 1)
	emu.SetScrollbackSize(0)
	_, _ = emu.Write([]byte("\x1b[31mAAAA\x1b[0m"))
	row := encodeRow(emu, 0, 8)
	if !strings.Contains(row, "AAAA") {
		t.Fatalf("missing text: %q", row)
	}
	if strings.Count(row, "\x1b[0m") > 1 {
		t.Fatalf("reset between same-style cells: %q", row)
	}
}

type firstWriteRecorder struct {
	first int
	n     int
}

func (w *firstWriteRecorder) Write(p []byte) (int, error) {
	if w.n == 0 {
		w.first = len(p)
	}
	w.n++
	return len(p), nil
}

func TestStreamSnapshotWritesHeaderFirst(t *testing.T) {
	s := newShadow(80, 24)
	s.Write([]byte("hello"))
	var w firstWriteRecorder
	s.streamSnapshot(&w)
	if w.n < 2 {
		t.Fatalf("expected header then rows, got %d writes", w.n)
	}
	if w.first != 8 {
		t.Fatalf("first write should be 8-byte header, got %d", w.first)
	}
}

func TestOuterWinsizeNil(t *testing.T) {
	c, r := outerWinsize(nil, nil)
	if c != 0 || r != 0 {
		t.Fatalf("cols=%d rows=%d", c, r)
	}
}

func TestOpenOuterTTY(t *testing.T) {
	in, out, err := openOuterTTY()
	if err != nil {
		t.Skip(err)
	}
	defer in.Close()
	defer out.Close()
	if in.Fd() == out.Fd() {
		t.Fatal("read and write sides must be distinct fds")
	}
	c, r := outerWinsize(in, out)
	if c < 1 || r < 1 {
		t.Fatalf("winsize cols=%d rows=%d", c, r)
	}
}
