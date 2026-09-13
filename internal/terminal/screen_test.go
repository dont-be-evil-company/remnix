//go:build unix

package terminal

import (
	"bytes"
	"strings"
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
	t.Cleanup(s.Close)
	if s.IsAltScreen() {
		t.Fatal("fresh emulator is on the main screen")
	}
	events := s.Write([]byte("\x1b[?2026"))
	entered, left := altHops(events)
	if entered || left || s.IsAltScreen() {
		t.Fatal("incomplete CSI must not switch screens")
	}
	events = s.Write([]byte("h\x1b[?1;1049;2004h"))
	entered, left = altHops(events)
	if !entered || left || !s.IsAltScreen() {
		t.Fatalf("combined 1049h should enter alt, entered=%v left=%v alt=%v events=%v", entered, left, s.IsAltScreen(), events)
	}
	events = s.Write([]byte("\x1b[?2026\x1b[?1049l"))
	entered, left = altHops(events)
	if entered || !left || s.IsAltScreen() {
		t.Fatalf("nested ESC then 1049l should leave alt, entered=%v left=%v alt=%v", entered, left, s.IsAltScreen())
	}

	s2 := newScreen(80, 24)
	t.Cleanup(s2.Close)
	events = s2.Write([]byte("\x1b[?1049h\x1b[?1049lLEFTALT"))
	entered, left = altHops(events)
	if !entered || !left || s2.IsAltScreen() {
		t.Fatalf("coalesced 1049h+1049l must report leave-alt, entered=%v left=%v alt=%v", entered, left, s2.IsAltScreen())
	}
}

func TestScreenAltScreenEvents(t *testing.T) {
	s := newScreen(80, 24)
	t.Cleanup(s.Close)

	got := altKinds(s.Write([]byte("\x1b[?1049h")))
	if !equalKinds(got, []TerminalEventKind{TerminalEventAltScreenEntered}) {
		t.Fatalf("enter: %v", got)
	}
	if !s.IsAltScreen() {
		t.Fatal("expected alt screen after 1049h")
	}

	got = altKinds(s.Write([]byte("\x1b[?1049l")))
	if !equalKinds(got, []TerminalEventKind{TerminalEventAltScreenLeft}) {
		t.Fatalf("leave: %v", got)
	}

	got = altKinds(s.Write([]byte("\x1b[?1049h\x1b[?1049l")))
	if !equalKinds(got, []TerminalEventKind{TerminalEventAltScreenEntered, TerminalEventAltScreenLeft}) {
		t.Fatalf("enter+leave in one write: %v", got)
	}

	got = altKinds(s.Write([]byte("\x1b[?1049h\x1b[?1049l\x1b[?1049h\x1b[?1049l")))
	want := []TerminalEventKind{
		TerminalEventAltScreenEntered, TerminalEventAltScreenLeft,
		TerminalEventAltScreenEntered, TerminalEventAltScreenLeft,
	}
	if !equalKinds(got, want) {
		t.Fatalf("multiple toggles: %v", got)
	}
}

func TestScreenAltScreenSplitSequence(t *testing.T) {
	s := newScreen(80, 24)
	t.Cleanup(s.Close)
	if kinds := altKinds(s.Write([]byte("\x1b[?1049"))); len(kinds) != 0 || s.IsAltScreen() {
		t.Fatalf("split prefix must not enter alt, events=%v alt=%v", kinds, s.IsAltScreen())
	}
	got := altKinds(s.Write([]byte("h")))
	if !equalKinds(got, []TerminalEventKind{TerminalEventAltScreenEntered}) || !s.IsAltScreen() {
		t.Fatalf("completing 1049h must enter alt, events=%v alt=%v", got, s.IsAltScreen())
	}
	if kinds := altKinds(s.Write([]byte("\x1b[?10"))); len(kinds) != 0 {
		t.Fatalf("partial leave must not emit, %v", kinds)
	}
	got = altKinds(s.Write([]byte("49l")))
	if !equalKinds(got, []TerminalEventKind{TerminalEventAltScreenLeft}) || s.IsAltScreen() {
		t.Fatalf("completing 1049l must leave alt, events=%v alt=%v", got, s.IsAltScreen())
	}
}

func TestIdleResetFromModeBits(t *testing.T) {
	s := newScreen(80, 24)
	t.Cleanup(s.Close)
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
	t.Cleanup(s.Close)
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
	t.Cleanup(s.Close)
	s.Write([]byte("\x1b[?1049h\x1b[?1049l"))
	got := idleReset(s, idleResetOpts{})
	if bytes.Contains(got, []byte("1049l")) {
		t.Fatalf("includeAlt false must not emit 1049l, got %q", got)
	}
}

func TestScreenSnapshotCoalescesStyle(t *testing.T) {
	s := newScreen(8, 1)
	t.Cleanup(s.Close)
	s.Write([]byte("\x1b[31mAAAA\x1b[0m"))
	row := s.Snapshot().RowANSI[0]
	if !strings.Contains(row, "AAAA") {
		t.Fatalf("missing text: %q", row)
	}
	nReset := strings.Count(row, "\x1b[0m") + strings.Count(row, ansi.ResetStyle)
	if nReset > 1 {
		t.Fatalf("reset between same-style cells (%d): %q", nReset, row)
	}
	if !strings.Contains(row, ansi.ResetStyle) && !strings.Contains(row, "\x1b[0m") {
		t.Fatalf("styled run must end with a reset, got %q", row)
	}
}

func TestScreenSnapshotWideCell(t *testing.T) {
	s := newScreen(8, 1)
	t.Cleanup(s.Close)
	s.Write([]byte("你A"))
	row := s.Snapshot().RowANSI[0]
	if !strings.Contains(row, "你") || !strings.Contains(row, "A") {
		t.Fatalf("missing glyphs: %q", row)
	}
	plain := stripSGR(row)
	if !strings.HasPrefix(strings.TrimRight(plain, " "), "你A") {
		t.Fatalf("wide cell must not insert a placeholder space, got %q", row)
	}
}

func altHops(events []TerminalEvent) (entered, left bool) {
	for _, ev := range events {
		switch ev.Kind {
		case TerminalEventAltScreenEntered:
			entered = true
		case TerminalEventAltScreenLeft:
			left = true
		}
	}
	return entered, left
}

func altKinds(events []TerminalEvent) []TerminalEventKind {
	var out []TerminalEventKind
	for _, ev := range events {
		if ev.Kind == TerminalEventAltScreenEntered || ev.Kind == TerminalEventAltScreenLeft {
			out = append(out, ev.Kind)
		}
	}
	return out
}

func equalKinds(got, want []TerminalEventKind) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func BenchmarkScreenWrite(b *testing.B) {
	s := newScreen(80, 24)
	b.Cleanup(s.Close)
	data := bytes.Repeat([]byte("abcdefghijklmnop\r\n"), 50)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.Write(data)
	}
}

func stripSGR(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
