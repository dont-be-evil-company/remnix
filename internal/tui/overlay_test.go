package tui

import (
	"strings"
	"testing"
)

func TestOverlayRowsFullByDefault(t *testing.T) {
	if got := overlayRows(24, 0); got != 24 {
		t.Fatalf("unset percent: %d", got)
	}
	if got := overlayRows(24, 100); got != 24 {
		t.Fatalf("100 percent: %d", got)
	}
}

func TestOverlayRowsPercentAndFloor(t *testing.T) {
	if got := overlayRows(24, 40); got != 10 { // 24*40/100 rounded
		t.Fatalf("40 percent of 24: %d", got)
	}
	if got := overlayRows(24, 10); got != SearchMinOverlayHeight {
		t.Fatalf("tiny percent should floor to min %d, got %d", SearchMinOverlayHeight, got)
	}
	if SearchMinOverlayHeight != SearchChromeRows+SearchListMinRows {
		t.Fatalf("min overlay %d != chrome %d + list %d", SearchMinOverlayHeight, SearchChromeRows, SearchListMinRows)
	}
	if got := overlayRows(8, 10); got != 8 {
		t.Fatalf("short terminal should not exceed rows: %d", got)
	}
	if got := overlayRows(40, 50); got != 20 {
		t.Fatalf("50 percent of 40: %d", got)
	}
}

func TestComposeOverlayPinsBody(t *testing.T) {
	bg := []string{"a", "b", "c", "d", "e", "f"}
	got := composeOverlay(6, 3, 2, bg, "TUI1\nTUI2")
	want := "a\nb\nc\nTUI1\nTUI2\nf"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestComposeOverlayFullIsIdentity(t *testing.T) {
	body := "one\ntwo\nthree"
	if got := composeOverlay(3, 0, 3, nil, body); got != body {
		t.Fatalf("got %q", got)
	}
}

func TestSuspendShellKeyboardPushesAndPops(t *testing.T) {
	var b strings.Builder
	restore := suspendShellKeyboard(&b)
	got := b.String()
	if !strings.Contains(got, "\x1b[>u") {
		t.Fatalf("must push kitty flags 0 so fish 4 CSI-u keys become UTF-8: %q", got)
	}
	if !strings.Contains(got, "\x1b[>4;0m") {
		t.Fatalf("must disable modifyOtherKeys: %q", got)
	}
	b.Reset()
	restore()
	got = b.String()
	if !strings.Contains(got, "\x1b[<1u") {
		t.Fatalf("must pop kitty flags so the parent shell keeps its protocol: %q", got)
	}
}

func TestDrawFixedCUPsEachRow(t *testing.T) {
	var b strings.Builder
	drawFixed(&b, 8, 3, 2, "AA\nBB")
	out := b.String()
	if !strings.Contains(out, "\x1b[4;1H") || !strings.Contains(out, "\x1b[5;1H") {
		t.Fatalf("missing absolute CUP for overlay rows: %q", out)
	}
	if !strings.Contains(out, "AA") || !strings.Contains(out, "BB") {
		t.Fatalf("missing body: %q", out)
	}
}

func TestSearchViewOverlayIsInlineNotAltScreen(t *testing.T) {
	m := New(nil, Options{OverlayPercent: 50})
	m.EnableOverlay(OverlayState{
		TermCols: 40,
		TermRows: 12,
		RectY:    6,
		RectH:    6,
	})
	v := m.View()
	if v.AltScreen {
		t.Fatal("Atuin popup mode is inline (Viewport::Fixed), not alt-screen")
	}
	lines := strings.Split(v.Content, "\n")
	if len(lines) > 7 {
		t.Fatalf("overlay view should be RectH lines, got %d: %q", len(lines), v.Content)
	}
	if !strings.Contains(lines[0], "syncsh") {
		t.Fatalf("tui should start at line 0 of the popup, got %q", lines[0])
	}
}
