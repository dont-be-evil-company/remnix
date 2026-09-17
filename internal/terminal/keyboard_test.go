package terminal

import (
	"bytes"
	"testing"
)

func TestKittyKeyDecoderLetters(t *testing.T) {
	var d kittyKeyDecoder
	got := d.feed([]byte("\x1b[97u\x1b[98u"))
	if string(got) != "ab" {
		t.Fatalf("got %q", got)
	}
}

func TestKittyKeyDecoderDropsRelease(t *testing.T) {
	var d kittyKeyDecoder
	got := d.feed([]byte("a\x1b[97;1:3u"))
	if string(got) != "a" {
		t.Fatalf("got %q", got)
	}
}

func TestKittyKeyDecoderDropsProtocolReplies(t *testing.T) {
	var d kittyKeyDecoder
	got := d.feed([]byte("x\x1b[?0u\x1b[<1uy"))
	if string(got) != "xy" {
		t.Fatalf("got %q", got)
	}
}

func TestKittyKeyDecoderSplit(t *testing.T) {
	var d kittyKeyDecoder
	if got := d.feed([]byte("\x1b[9")); len(got) != 0 {
		t.Fatalf("partial %q", got)
	}
	got := d.feed([]byte("7u!"))
	if string(got) != "a!" {
		t.Fatalf("got %q", got)
	}
}

func TestKittyKeyDecoderC0CancelsIncompleteCSI(t *testing.T) {
	var d kittyKeyDecoder
	if got := d.feed([]byte("\x1b[")); len(got) != 0 {
		t.Fatalf("partial %q", got)
	}
	got := d.feed([]byte{3})
	if !bytes.Equal(got, []byte{0x1b, '[', 3}) {
		t.Fatalf("Ctrl+C must flush held CSI then the C0, got %q", got)
	}
}

func TestKittyKeyDecoderNestedEscCancelsCSI(t *testing.T) {
	var d kittyKeyDecoder
	got := d.feed([]byte("\x1b[\x1b[Ia"))
	if string(got) != "\x1b[a" {
		t.Fatalf("nested ESC must flush held CSI then drop focus, got %q", got)
	}
}

func TestKittyKeyDecoderDropsFocus(t *testing.T) {
	var d kittyKeyDecoder
	got := d.feed([]byte("a\x1b[I\x1b[Ob"))
	if string(got) != "ab" {
		t.Fatalf("focus CSI I/O must not reach the shell, got %q", got)
	}
}

func TestKittyKeyDecoderLoneEscape(t *testing.T) {
	var d kittyKeyDecoder
	got := d.feed([]byte{0x1b})
	if !bytes.Equal(got, []byte{0x1b}) {
		t.Fatalf("lone ESC must not be held, got %q", got)
	}
	got = d.feed([]byte("x"))
	if string(got) != "x" {
		t.Fatalf("after ESC, next key %q", got)
	}
}

func TestKittyKeyDecoderCtrlC(t *testing.T) {
	var d kittyKeyDecoder
	got := d.feed([]byte("\x1b[99;5u"))
	if !bytes.Equal(got, []byte{3}) {
		t.Fatalf("got %q", got)
	}
}

func TestKittyKeyDecoderPlainPassthrough(t *testing.T) {
	var d kittyKeyDecoder
	got := d.feed([]byte("hello\n"))
	if string(got) != "hello\n" {
		t.Fatalf("got %q", got)
	}
}

func TestKeyboardModeStripper(t *testing.T) {
	var s keyboardModeStripper
	in := []byte("hi\x1b[>3u\x1b[<1u\x1b[=0;1u\x1b[>4;2m\x1b[>4;0m\x1b[31mthere")
	got := s.feed(in)
	if bytes.Contains(got, []byte("\x1b[>3u")) || bytes.Contains(got, []byte("\x1b[>4;2m")) {
		t.Fatalf("enable CSI leaked: %q", got)
	}
	if !bytes.Contains(got, []byte("\x1b[<1u")) || !bytes.Contains(got, []byte("\x1b[=0;1u")) || !bytes.Contains(got, []byte("\x1b[>4;0m")) {
		t.Fatalf("disable must reach the emulator: %q", got)
	}
	if !bytes.Contains(got, []byte("\x1b[31m")) || !bytes.Contains(got, []byte("hi")) || !bytes.Contains(got, []byte("there")) {
		t.Fatalf("kept payload missing: %q", got)
	}
}

func TestRewriteFocusTrackingCombined(t *testing.T) {
	got, drop, off := rewriteFocusTracking([]byte("\x1b[?1;1004;2004h"))
	if drop || !off || string(got) != "\x1b[?1;2004h" {
		t.Fatalf("got %q drop=%v off=%v", got, drop, off)
	}
	got, drop, off = rewriteFocusTracking([]byte("\x1b[?1004h"))
	if !drop || !off || got != nil {
		t.Fatalf("bare 1004h drop=%v off=%v %q", drop, off, got)
	}
	got, drop, off = rewriteFocusTracking([]byte("\x1b[?1;2004h"))
	if drop || off || string(got) != "\x1b[?1;2004h" {
		t.Fatalf("unrelated modes %q", got)
	}
	got, drop, off = rewriteFocusTracking([]byte("\x1b[?1004l"))
	if drop || off || string(got) != "\x1b[?1004l" {
		t.Fatalf("bare 1004l drop=%v off=%v %q", drop, off, got)
	}
}

func TestKeyboardModeStripperDropsTmuxFocus(t *testing.T) {
	var s keyboardModeStripper
	got := s.feed([]byte("x\x1b[?1;1004;2004hy"))
	if bytes.Contains(got, []byte("1004h")) {
		t.Fatalf("1004h leaked: %q", got)
	}
	if !bytes.Contains(got, []byte("\x1b[?1;2004h")) || !bytes.Contains(got, []byte("\x1b[?1004l")) {
		t.Fatalf("must keep other modes and force focus off: %q", got)
	}
	if !bytes.Contains(got, []byte("x")) || !bytes.Contains(got, []byte("y")) {
		t.Fatalf("payload: %q", got)
	}
}

func TestKeyboardModeStripperSplit(t *testing.T) {
	var s keyboardModeStripper
	if got := s.feed([]byte("\x1b[>3")); bytes.Contains(got, []byte("\x1b[>3")) {
		t.Fatalf("partial must wait, got %q", got)
	}
	got := s.feed([]byte("uok"))
	if string(got) != "ok" {
		t.Fatalf("got %q", got)
	}
}

func TestKeyboardModeStripperAbortsCSIOnEsc(t *testing.T) {
	var s keyboardModeStripper
	got := s.feed([]byte("\x1b[?2026\x1b[?1049l\x1b[?25h"))
	if !bytes.Contains(got, []byte("\x1b[?1049l")) {
		t.Fatalf("nested ESC must not swallow 1049l, got %q", got)
	}
	if !bytes.Contains(got, []byte("\x1b[?25h")) {
		t.Fatalf("must keep next CSI, got %q", got)
	}
}

func TestKittyKeyDecoderOverflowFlushesAll(t *testing.T) {
	var d kittyKeyDecoder
	held := append([]byte{0x1b, '['}, bytes.Repeat([]byte("1"), kittyHoldMax)...)
	got := d.feed(held)
	if !bytes.Equal(got, held) {
		t.Fatalf("overflow must flush all held bytes, got %d want %d", len(got), len(held))
	}
	if len(d.hold) != 0 {
		t.Fatalf("hold after overflow: %q", d.hold)
	}
}

func TestKittyKeyDecoderUnknownCSIForwarded(t *testing.T) {
	var d kittyKeyDecoder
	seq := []byte("\x1b[999;999~")
	got := d.feed(seq)
	if !bytes.Equal(got, seq) {
		t.Fatalf("unknown CSI must pass through, got %q", got)
	}
}

func TestKittyKeyDecoderCSIUSplitEveryOffset(t *testing.T) {
	seq := []byte("\x1b[97;5uX")
	var whole kittyKeyDecoder
	want := whole.feed(seq)
	if string(want) != "aX" && string(want) != "\x01X" {
		// 97;5u is 'a' with ctrl? 97 is 'a', mods 5 = ctrl → \x01
		if string(want) != "\x01X" {
			t.Fatalf("unexpected decode %q", want)
		}
	}
	for split := 2; split < len(seq); split++ {
		var d kittyKeyDecoder
		got := append([]byte(nil), d.feed(seq[:split])...)
		got = append(got, d.feed(seq[split:])...)
		if !bytes.Equal(got, want) {
			t.Fatalf("split %d: got %q want %q", split, got, want)
		}
	}
}
