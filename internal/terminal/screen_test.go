//go:build unix

package terminal

import (
	"bytes"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestScreenWriteDADoesNotBlock(t *testing.T) {
	s := newScreen(80, 24)
	t.Cleanup(s.Close)
	done := make(chan struct{})
	go func() {
		s.Write([]byte("\x1b[c\x1b[6n\x1b[?1049h"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Write blocked on vt.Emulator input pipe (DA/CPR reply)")
	}
	if !s.IsAltScreen() {
		t.Fatal("1049h after DA must still enter alt")
	}
}

func TestScreenAltScreenSplitAndCombined(t *testing.T) {
	s := newScreen(80, 24)
	if s.IsAltScreen() {
		t.Fatal("fresh emulator is on the main screen")
	}
	entered, left := s.Write([]byte("\x1b[?2026"))
	if entered || left || s.IsAltScreen() {
		t.Fatal("incomplete CSI must not switch screens")
	}
	entered, left = s.Write([]byte("h\x1b[?1;1049;2004h"))
	if !entered || left || !s.IsAltScreen() {
		t.Fatalf("combined 1049h should enter alt, entered=%v left=%v alt=%v", entered, left, s.IsAltScreen())
	}
	entered, left = s.Write([]byte("\x1b[?2026\x1b[?1049l"))
	if entered || !left || s.IsAltScreen() {
		t.Fatalf("nested ESC then 1049l should leave alt, entered=%v left=%v alt=%v", entered, left, s.IsAltScreen())
	}
}

func TestIdleResetFromModeBits(t *testing.T) {
	s := newScreen(80, 24)
	s.Write([]byte("\x1b[?1049h\x1b[?2026h\x1b[?1000h"))
	if !s.IsAltScreen() {
		t.Fatal("expected alt screen")
	}
	got := idleReset(s, idleResetOpts{includeAlt: true})
	if !bytes.Contains(got, []byte(ansi.ResetMode(ansi.ModeAltScreenSaveCursor))) {
		t.Fatalf("still on alt: must emit 1049l, got %q", got)
	}
	if !bytes.Contains(got, []byte(ansi.ResetMode(ansi.ModeSynchronizedOutput))) {
		t.Fatalf("must reset 2026, got %q", got)
	}
	if !bytes.Contains(got, []byte(ansi.ResetMode(ansi.ModeMouseNormal))) {
		t.Fatalf("must reset 1000, got %q", got)
	}
	if !bytes.Contains(got, []byte(seqKittyPop)) || !bytes.Contains(got, []byte(seqModifyKeysOff)) {
		t.Fatalf("must pop kitty keyboard, got %q", got)
	}

	s.Write(got)
	if s.IsAltScreen() {
		t.Fatal("idleReset bytes must take the emulator off alt")
	}
	again := idleReset(s, idleResetOpts{})
	if len(again) != 0 {
		t.Fatalf("second call must be empty, got %q", again)
	}
}

func TestIdleResetHardKillStillOnAlt(t *testing.T) {
	s := newScreen(80, 24)
	s.Write([]byte("\x1b[?1049h\x1b[?1000h"))
	got := idleReset(s, idleResetOpts{includeAlt: true})
	if !bytes.Contains(got, []byte("1049l")) {
		t.Fatalf("still on alt after process gone: must emit 1049l, got %q", got)
	}
	s.Write(got)
	if s.IsAltScreen() {
		t.Fatal("expected main screen after includeAlt reset")
	}
}

func TestIdleResetNoAltWhenAlreadyLeft(t *testing.T) {
	s := newScreen(80, 24)
	s.Write([]byte("\x1b[?1049h\x1b[?1049l"))
	got := idleReset(s, idleResetOpts{})
	if bytes.Contains(got, []byte("1049l")) {
		t.Fatalf("includeAlt false must not emit 1049l, got %q", got)
	}
}
