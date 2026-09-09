package terminal

import (
	"bytes"
	"testing"
)

func TestQueryScannerPrimaryDA(t *testing.T) {
	var q queryScanner
	got := q.feed([]byte("hello\x1b[0cworld"), 24, 80, 0, 0)
	if !bytes.Equal(got, []byte("\x1b[?1;2c")) {
		t.Fatalf("DA reply %q", got)
	}
	got = q.feed([]byte("\x1b[c"), 24, 80, 0, 0)
	if !bytes.Equal(got, []byte("\x1b[?1;2c")) {
		t.Fatalf("short DA reply %q", got)
	}
}

func TestQueryScannerSplitDA(t *testing.T) {
	var q queryScanner
	if reply := q.feed([]byte("\x1b[0"), 24, 80, 0, 0); len(reply) != 0 {
		t.Fatalf("partial DA should wait, got %q", reply)
	}
	got := q.feed([]byte("c"), 24, 80, 0, 0)
	if !bytes.Equal(got, []byte("\x1b[?1;2c")) {
		t.Fatalf("split DA reply %q", got)
	}
}

func TestQueryScannerCPRAndKitty(t *testing.T) {
	var q queryScanner
	got := q.feed([]byte("\x1b[6n\x1b[>u\x1b[=1u"), 24, 80, 2, 10)
	want := []byte("\x1b[3;11R")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestQueryScannerIgnoresXTVERSION(t *testing.T) {
	var q queryScanner
	got := q.feed([]byte("\x1b[>0q"), 24, 80, 0, 0)
	if len(got) != 0 {
		t.Fatalf("XTVERSION must not inject DCS, got %q", got)
	}
}

func TestQueryScannerKittyKeyboardQuery(t *testing.T) {
	var q queryScanner
	got := q.feed([]byte("\x1b[?u\x1b[c"), 24, 80, 0, 0)
	want := []byte("\x1b[?0u\x1b[?1;2c")
	if !bytes.Equal(got, want) {
		t.Fatalf("nvim handshake reply %q want %q", got, want)
	}
	got = q.feed([]byte("\x1b[>3u\x1b[=1;1u"), 24, 80, 0, 0)
	if len(got) != 0 {
		t.Fatalf("push/set must not be answered, got %q", got)
	}
}
