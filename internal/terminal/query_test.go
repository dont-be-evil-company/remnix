package terminal

import (
	"bytes"
	"testing"
)

func TestQueryScannerPrimaryDA(t *testing.T) {
	var q queryScanner
	got := q.feed([]byte("hello\x1b[0cworld"), 24, 80)
	if !bytes.Equal(got, []byte("\x1b[?1;2c")) {
		t.Fatalf("DA reply %q", got)
	}
	got = q.feed([]byte("\x1b[c"), 24, 80)
	if !bytes.Equal(got, []byte("\x1b[?1;2c")) {
		t.Fatalf("short DA reply %q", got)
	}
}

func TestQueryScannerSplitDA(t *testing.T) {
	var q queryScanner
	if reply := q.feed([]byte("\x1b[0"), 24, 80); len(reply) != 0 {
		t.Fatalf("partial DA should wait, got %q", reply)
	}
	got := q.feed([]byte("c"), 24, 80)
	if !bytes.Equal(got, []byte("\x1b[?1;2c")) {
		t.Fatalf("split DA reply %q", got)
	}
}

func TestQueryScannerCPRAndKitty(t *testing.T) {
	var q queryScanner
	got := q.feed([]byte("\x1b[6n\x1b[>u\x1b[=1u"), 24, 80)
	want := []byte("\x1b[24;80R")
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestQueryScannerIgnoresXTVERSION(t *testing.T) {
	var q queryScanner
	got := q.feed([]byte("\x1b[>0q"), 24, 80)
	if len(got) != 0 {
		t.Fatalf("XTVERSION must not inject DCS, got %q", got)
	}
}

func TestQueryScannerKittyKeyboardQuery(t *testing.T) {
	var q queryScanner
	got := q.feed([]byte("\x1b[?u\x1b[c"), 24, 80)
	want := []byte("\x1b[?0u\x1b[?1;2c")
	if !bytes.Equal(got, want) {
		t.Fatalf("nvim handshake reply %q want %q", got, want)
	}
	got = q.feed([]byte("\x1b[>3u\x1b[=1;1u"), 24, 80)
	if len(got) != 0 {
		t.Fatalf("push/set must not be answered, got %q", got)
	}
}

func TestLeavesAltScreen(t *testing.T) {
	if !leavesAltScreen([]byte("\x1b[?1049l")) {
		t.Fatal("1049l")
	}
	if !leavesAltScreen([]byte("done\x1b[?1049;2004l")) {
		t.Fatal("combined 1049l")
	}
	if !leavesAltScreen([]byte("\x1b[?47l")) {
		t.Fatal("47l")
	}
	if leavesAltScreen([]byte("\x1b[?1049h")) {
		t.Fatal("enter alt screen is not leave")
	}
	if leavesAltScreen([]byte("hello")) {
		t.Fatal("plain text")
	}
}

func TestWithMainScreenKeyboardReset(t *testing.T) {
	in := []byte("bye\x1b[?1049l")
	got := withMainScreenKeyboardReset(in)
	if !bytes.HasPrefix(got, in) {
		t.Fatalf("must keep original bytes, got %q", got)
	}
	if !bytes.Contains(got, []byte(seqKittyFlagsOff)) || !bytes.Contains(got, []byte(seqModifyKeysOff)) {
		t.Fatalf("must disable kitty + modifyOtherKeys after nvim, got %q", got)
	}
	plain := []byte("hello")
	if got := withMainScreenKeyboardReset(plain); !bytes.Equal(got, plain) {
		t.Fatalf("unchanged without alt-screen leave, got %q", got)
	}
}

func TestAltLeaveWatchSplitSequence(t *testing.T) {
	var w altLeaveWatch
	got, left := w.feed([]byte("\x1b[?10"))
	if left || bytes.Contains(got, []byte(seqKittyFlagsOff)) {
		t.Fatalf("partial CSI must wait, got %q", got)
	}
	got, left = w.feed([]byte("49l"))
	if !left || !bytes.Contains(got, []byte(seqKittyFlagsOff)) || !bytes.Contains(got, []byte(seqModifyKeysOff)) {
		t.Fatalf("split rmcup should reset keyboard, got %q", got)
	}
	got, left = w.feed([]byte("ok"))
	if left || bytes.Contains(got, []byte(seqKittyFlagsOff)) {
		t.Fatalf("must not reset twice, got %q", got)
	}
}
