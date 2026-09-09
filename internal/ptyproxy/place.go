package ptyproxy

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Rect is a 0-indexed screen rectangle.
type Rect struct {
	X, Y, W, H int
}

// Placement is where an overlay should be drawn and how far the terminal was
// (or should be) scrolled to make room.
type Placement struct {
	Rect   Rect
	Scroll int
}

// Place computes overlay position. Matches Atuin's rules: draw below the
// cursor when it fits; above when the cursor is in the bottom half; otherwise
// scroll the terminal down.
func Place(cursorRow, termRows, termCols, height int) Placement {
	if termRows < 1 {
		termRows = 1
	}
	if termCols < 1 {
		termCols = 1
	}
	if height < 1 {
		height = 1
	}
	if height > termRows {
		height = termRows
	}
	spaceBelow := termRows - cursorRow
	if spaceBelow < 0 {
		spaceBelow = 0
	}
	switch {
	case height <= spaceBelow:
		return Placement{Rect: Rect{X: 0, Y: cursorRow, W: termCols, H: height}}
	case cursorRow >= termRows/2:
		y := cursorRow - height
		if y < 0 {
			y = 0
		}
		return Placement{Rect: Rect{X: 0, Y: y, W: termCols, H: height}}
	default:
		scroll := height - spaceBelow
		if scroll < 0 {
			scroll = 0
		}
		y := cursorRow - scroll
		if y < 0 {
			y = 0
		}
		return Placement{Rect: Rect{X: 0, Y: y, W: termCols, H: height}, Scroll: scroll}
	}
}

// ContentCursorRow pins a snapshot cursor that sits on blank cells below the
// prompt. Fish (no POSTDISPLAY) often leaves the emulator/widget cursor on the
// last row while the command line is still near the top; zsh redisplay keeps
// them aligned so this is a no-op there.
func ContentCursorRow(s Snapshot) int {
	last := -1
	for i, row := range s.RowANSI {
		if rowHasPrintable(row) {
			last = i
		}
	}
	if last < 0 {
		return s.CursorRow
	}
	if s.CursorRow > last {
		return last
	}
	return s.CursorRow
}

func rowHasPrintable(row string) bool {
	for _, r := range ansi.Strip(row) {
		if !unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

// MoveTo writes a 1-indexed CUP sequence.
func MoveTo(w io.Writer, col, row int) {
	if col < 0 {
		col = 0
	}
	if row < 0 {
		row = 0
	}
	_, _ = fmt.Fprintf(w, "\x1b[%d;%dH", row+1, col+1)
}

// ScrollUp writes newlines at the bottom of the screen so the viewport moves
// up by n rows.
func ScrollUp(w io.Writer, termRows, n int) {
	if n <= 0 || termRows < 1 {
		return
	}
	MoveTo(w, 0, termRows-1)
	_, _ = io.WriteString(w, strings.Repeat("\n", n))
}

// ClearRect fills a rectangle with spaces (reset attributes) so leftover
// cells cannot show through a TUI that diffs against empty.
func ClearRect(w io.Writer, r Rect) {
	if r.W < 1 || r.H < 1 {
		return
	}
	blank := strings.Repeat(" ", r.W)
	for dy := 0; dy < r.H; dy++ {
		MoveTo(w, r.X, r.Y+dy)
		_, _ = io.WriteString(w, "\x1b[0m")
		_, _ = io.WriteString(w, blank)
	}
}

// Restore rewrites saved rows over the overlay rectangle and puts the cursor
// back. scroll is the number of lines Prepare scrolled the terminal.
func (s Snapshot) Restore(w io.Writer, r Rect, scroll int) {
	if r.W < 1 || r.H < 1 {
		return
	}
	blank := strings.Repeat(" ", r.W)
	for dy := 0; dy < r.H; dy++ {
		target := r.Y + dy
		MoveTo(w, r.X, target)
		_, _ = io.WriteString(w, "\x1b[0m")
		_, _ = io.WriteString(w, blank)
		MoveTo(w, r.X, target)
		src := target + scroll
		if src >= 0 && src < len(s.RowANSI) {
			_, _ = io.WriteString(w, s.RowANSI[src])
		}
	}
	row := s.CursorRow - scroll
	if row < 0 {
		row = 0
	}
	MoveTo(w, s.CursorCol, row)
	// drawFixed hides the cursor (DECTCEM). Bubble Tea cannot restore it:
	// overlay output is Discard, so the renderer never emits ?25h.
	_, _ = io.WriteString(w, "\x1b[?25h")
}

// Prepare matches Atuin interactive.rs popup setup: scroll if the overlay
// does not fit, clear the popup rectangle, then CUP to its origin.
// Ratatui Viewport::Fixed CUPs every frame; Bubble Tea's inline renderer
// only paints relative to the current cursor, so the CUP is required.
func Prepare(w io.Writer, snap Snapshot, place Placement) {
	if place.Scroll > 0 {
		ScrollUp(w, snap.Rows, place.Scroll)
	}
	ClearRect(w, place.Rect)
	MoveTo(w, place.Rect.X, place.Rect.Y)
	if f, ok := w.(interface{ Flush() error }); ok {
		_ = f.Flush()
	}
}
