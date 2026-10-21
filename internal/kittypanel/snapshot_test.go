package kittypanel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitScreenDropsTrailingNewline(t *testing.T) {
	got := SplitScreen("alpha\nbravo\n")
	if len(got) != 2 || got[0] != "alpha" || got[1] != "bravo" {
		t.Fatalf("rows: %#v", got)
	}
	blank := SplitScreen("alpha\n\ncharlie\n")
	if len(blank) != 3 || blank[1] != "" {
		t.Fatalf("blank row: %#v", blank)
	}
	if len(SplitScreen("")) != 0 {
		t.Fatal("empty screen should be an empty slice, not nil")
	}
	if SplitScreen("") == nil {
		t.Fatal("empty screen must be non-nil so the panel still paints")
	}
}

func TestStripTrailingCursor(t *testing.T) {
	raw := "top\nprompt\n\n\x1b[?25h\x1b[2;5H\x1b[2 q"
	body, row, ok := stripTrailingCursor(raw)
	if !ok || row != 1 {
		t.Fatalf("ok=%v row=%d", ok, row)
	}
	lines := SplitScreen(body)
	if len(lines) != 3 || lines[1] != "prompt" {
		t.Fatalf("screen: %#v", lines)
	}
	if _, _, ok := stripTrailingCursor("just text\n"); ok {
		t.Fatal("plain text must not invent a cursor")
	}
}

func TestReportMissingSocket(t *testing.T) {
	t.Setenv("KITTY_LISTEN_ON", "")
	ok, msg := Report()
	if ok || !strings.Contains(msg, "KITTY_LISTEN_ON is unset") {
		t.Fatalf("ok=%v msg=%q", ok, msg)
	}
	missing := filepath.Join(t.TempDir(), "no-such.sock")
	t.Setenv("KITTY_LISTEN_ON", "unix:"+missing)
	ok, msg = Report()
	if ok || !strings.Contains(msg, "missing") {
		t.Fatalf("ok=%v msg=%q", ok, msg)
	}
	sock := filepath.Join(t.TempDir(), "kitty.sock")
	if err := os.WriteFile(sock, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KITTY_LISTEN_ON", "unix:"+sock)
	ok, msg = Report()
	if !ok || !strings.Contains(msg, sock) {
		t.Fatalf("ok=%v msg=%q", ok, msg)
	}
	t.Setenv("KITTY_LISTEN_ON", "unix:@remnix-kitty")
	ok, msg = Report()
	if !ok {
		t.Fatalf("abstract socket: %s", msg)
	}
}
