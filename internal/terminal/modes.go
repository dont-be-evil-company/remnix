//go:build unix

package terminal

import (
	"github.com/charmbracelet/x/ansi"
)

const (
	seqKittyFlagsOff = "\x1b[=0;1u"
	seqKittyPop      = "\x1b[<u\x1b[<1u"
	seqModifyKeysOff = "\x1b[>4;0m"
	seqAbortString   = "\x1b\\\x07"
)

// stickyModes are DEC private modes a shell must not inherit after a TUI.
// Sequences are generated with ansi.ResetMode; 1049 is added only when the
// emulator is still on the alternate screen.
var stickyModes = []ansi.Mode{
	ansi.ModeSynchronizedOutput,
	ansi.ModeBracketedPaste,
	ansi.ModeFocusEvent,
	ansi.ModeMouseNormal,
	ansi.ModeMouseButtonEvent,
	ansi.ModeMouseAnyEvent,
	ansi.ModeMouseExtSgr,
}

type idleResetOpts struct {
	includeAlt  bool
	hideCursor  bool
	forceSticky bool
}

func idleReset(s *Screen, opts idleResetOpts) []byte {
	var leftover []ansi.Mode
	onAlt := false
	needKB := false
	if s != nil {
		leftover, onAlt, needKB = s.idleSnapshot(opts.forceSticky)
	}
	dec := leftover
	if opts.includeAlt && onAlt {
		dec = appendMode(dec, ansi.ModeAltScreenSaveCursor)
	}
	emitKB := needKB || opts.forceSticky || len(dec) > 0
	if len(dec) == 0 && !emitKB && !opts.hideCursor {
		return nil
	}
	if s != nil && emitKB {
		s.clearKeyboardSticky()
	}
	out := make([]byte, 0, 96)
	out = append(out, seqAbortString...)
	for _, m := range dec {
		out = append(out, ansi.ResetMode(m)...)
	}
	if emitKB {
		out = append(out, seqKittyPop...)
		out = append(out, seqKittyFlagsOff...)
		out = append(out, seqModifyKeysOff...)
	}
	if opts.hideCursor {
		out = append(out, ansi.HideCursor...)
		out = append(out, "\x1b[0m"...)
	} else {
		out = append(out, ansi.ShowCursor...)
	}
	return out
}

func appendMode(modes []ansi.Mode, add ansi.Mode) []ansi.Mode {
	if add == nil {
		return modes
	}
	id := add.Mode()
	for _, m := range modes {
		if m != nil && m.Mode() == id {
			return modes
		}
	}
	return append(modes, add)
}
