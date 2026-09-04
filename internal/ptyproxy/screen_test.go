package ptyproxy

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := Snapshot{
		Rows:      3,
		Cols:      10,
		CursorRow: 1,
		CursorCol: 4,
		RowANSI:   []string{"\x1b[32mone\x1b[0m", "two", ""},
	}
	got, err := Decode(Encode(in))
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows != 3 || got.Cols != 10 || got.CursorRow != 1 || got.CursorCol != 4 {
		t.Fatalf("header: %+v", got)
	}
	if len(got.RowANSI) != 3 || got.RowANSI[0] != in.RowANSI[0] || got.RowANSI[1] != "two" || got.RowANSI[2] != "" {
		t.Fatalf("rows: %#v", got.RowANSI)
	}
}

func TestDecodeShort(t *testing.T) {
	if _, err := Decode([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeTruncatedRow(t *testing.T) {
	buf := Encode(Snapshot{Rows: 1, Cols: 1, RowANSI: []string{"hello"}})
	if _, err := Decode(buf[:len(buf)-1]); err == nil {
		t.Fatal("expected truncated row error")
	}
}

func TestFetchFromSocket(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pty.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	want := Snapshot{Rows: 2, Cols: 4, CursorRow: 1, CursorCol: 2, RowANSI: []string{"abcd", "efgh"}}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Write(Encode(want))
	}()
	t.Setenv(EnvSocket, path)
	got, err := Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if got.Rows != 2 || got.CursorCol != 2 || got.RowANSI[1] != "efgh" {
		t.Fatalf("%+v", got)
	}
}

func TestFetchDoesNotWaitForEOF(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pty.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	want := Snapshot{Rows: 1, Cols: 3, CursorRow: 0, CursorCol: 1, RowANSI: []string{"abc"}}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = c.Write(Encode(want))
		// Leave the connection open; ReadAll would block until the deadline.
	}()
	t.Setenv(EnvSocket, path)
	start := time.Now()
	got, err := Fetch()
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("Fetch waited %s for a close that never came", time.Since(start))
	}
	if got.RowANSI[0] != "abc" {
		t.Fatalf("%+v", got)
	}
}

func TestFetchGeomDoesNotWaitForRows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pty.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Skip(err)
	}
	defer ln.Close()
	hdr := EncodeHeader(Snapshot{Rows: 24, Cols: 80, CursorRow: 23, CursorCol: 0})
	done := make(chan struct{})
	defer close(done)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Write(hdr)
		<-done
	}()
	t.Setenv(EnvSocket, path)
	start := time.Now()
	got, rest, err := FetchGeom()
	if err != nil {
		t.Fatal(err)
	}
	defer rest.Close()
	if time.Since(start) > 300*time.Millisecond {
		t.Fatalf("FetchGeom waited %s for rows that were never sent", time.Since(start))
	}
	if got.Rows != 24 || got.Cols != 80 || got.CursorRow != 23 {
		t.Fatalf("%+v", got)
	}
}

func TestFetchNoProxy(t *testing.T) {
	t.Setenv(EnvSocket, "")
	if _, err := Fetch(); err != ErrNoProxy {
		t.Fatalf("got %v", err)
	}
}

func TestRestoreAndPlace(t *testing.T) {
	s := Snapshot{
		Rows:      6,
		Cols:      8,
		CursorRow: 2,
		CursorCol: 3,
		RowANSI:   []string{"aaaa", "bbbb", "cccc", "dddd", "eeee", "ffff"},
	}
	p := Place(2, 6, 8, 3)
	if p.Rect.Y != 2 || p.Rect.H != 3 || p.Scroll != 0 {
		t.Fatalf("place below: %+v", p)
	}
	var b strings.Builder
	s.Restore(&b, p.Rect, p.Scroll)
	out := b.String()
	if !strings.Contains(out, "cccc") || !strings.Contains(out, "dddd") {
		t.Fatalf("restore missing rows: %q", out)
	}
	if !strings.Contains(out, "\x1b[3;4H") { // cursor 2,3 → 1-indexed 3,4
		t.Fatalf("cursor not restored: %q", out)
	}
	if !strings.Contains(out, "\x1b[?25h") {
		t.Fatal("restore must show the cursor (overlay hides it with ?25l)")
	}
}

func TestPlaceAboveWhenBottomHalf(t *testing.T) {
	p := Place(20, 24, 80, 10)
	if p.Rect.Y != 10 || p.Scroll != 0 {
		t.Fatalf("expected above cursor, got %+v", p)
	}
}

func TestPlaceScrollWhenTopHalf(t *testing.T) {
	p := Place(5, 24, 80, 20)
	if p.Scroll != 1 || p.Rect.Y != 4 {
		t.Fatalf("expected scroll to make room, got %+v", p)
	}
}

func TestPlaceFitsBelow(t *testing.T) {
	p := Place(5, 24, 80, 8)
	if p.Rect.Y != 5 || p.Scroll != 0 || p.Rect.H != 8 || p.Rect.W != 80 {
		t.Fatalf("%+v", p)
	}
}
