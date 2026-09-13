package terminal

import (
	"bytes"
	"testing"
)

func TestQueryScannerPrimaryDA(t *testing.T) {
	var q queryScanner
	got := q.feed([]byte("hello\x1b[0cworld"))
	if !bytes.Equal(got.replies, []byte("\x1b[?1;2c")) {
		t.Fatalf("DA reply %q", got.replies)
	}
	got = q.feed([]byte("\x1b[c"))
	if !bytes.Equal(got.replies, []byte("\x1b[?1;2c")) {
		t.Fatalf("short DA reply %q", got.replies)
	}
}

func TestQueryScannerSplitDA(t *testing.T) {
	var q queryScanner
	if reply := q.feed([]byte("\x1b[0")); len(reply.replies) != 0 {
		t.Fatalf("partial DA should wait, got %q", reply.replies)
	}
	got := q.feed([]byte("c"))
	if !bytes.Equal(got.replies, []byte("\x1b[?1;2c")) {
		t.Fatalf("split DA reply %q", got.replies)
	}
}

func TestQueryScannerCPRIsNotFastPath(t *testing.T) {
	var q queryScanner
	got := q.feed([]byte("\x1b[6n\x1b[>u\x1b[=1u"))
	if len(got.replies) != 0 {
		t.Fatalf("CPR must not be answered on the fast path, got %q", got.replies)
	}
	if len(got.cprEnds) != 1 || got.cprEnds[0] != 4 {
		t.Fatalf("cprEnds %v want [4]", got.cprEnds)
	}
}

func TestQueryScannerSplitCPR(t *testing.T) {
	var q queryScanner
	if got := q.feed([]byte("\x1b[6")); len(got.cprEnds) != 0 || len(got.replies) != 0 {
		t.Fatalf("partial CPR should wait, %+v", got)
	}
	got := q.feed([]byte("nAFTER"))
	if len(got.replies) != 0 {
		t.Fatalf("split CPR must not fast-reply, %q", got.replies)
	}
	if len(got.cprEnds) != 1 || got.cprEnds[0] != 1 {
		t.Fatalf("cprEnds %v want [1] (end of n in this chunk)", got.cprEnds)
	}
}

func TestQueryScannerIgnoresXTVERSION(t *testing.T) {
	var q queryScanner
	got := q.feed([]byte("\x1b[>0q"))
	if len(got.replies) != 0 {
		t.Fatalf("XTVERSION must not inject DCS, got %q", got.replies)
	}
}

func TestQueryScannerKittyKeyboardQuery(t *testing.T) {
	var q queryScanner
	got := q.feed([]byte("\x1b[?u\x1b[c"))
	want := []byte("\x1b[?0u\x1b[?1;2c")
	if !bytes.Equal(got.replies, want) {
		t.Fatalf("keyboard/DA handshake reply %q want %q", got.replies, want)
	}
	got = q.feed([]byte("\x1b[>3u\x1b[=1;1u"))
	if len(got.replies) != 0 {
		t.Fatalf("push/set must not be answered, got %q", got.replies)
	}
}

func TestFormatCPR(t *testing.T) {
	got := formatCPR(24, 80, 9, 19)
	if !bytes.Equal(got, []byte("\x1b[10;20R")) {
		t.Fatalf("formatCPR %q", got)
	}
}
