//go:build unix

package terminal

import (
	"sync"
	"sync/atomic"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/dont-be-evil-company/remnix/internal/ptyproxy"
)

// TerminalEventKind is a semantic transition reported while parsing PTY bytes.
type TerminalEventKind uint8

const (
	TerminalEventModeEnabled TerminalEventKind = iota
	TerminalEventModeDisabled
	TerminalEventAltScreenEntered
	TerminalEventAltScreenLeft
)

// TerminalEvent is produced from VT emulator callbacks, not a second ANSI scan.
type TerminalEvent struct {
	Kind TerminalEventKind
	Mode ansi.Mode
}

var (
	modeAltScreenSaveCursor = ansi.ModeAltScreenSaveCursor // 1049
	modeAltScreen           = ansi.ModeAltScreen           // 1047
	modeAltScreenLegacy     = ansi.DECMode(47)
)

type Screen struct {
	mu           sync.Mutex
	emu          *vt.Emulator
	sticky       map[int]bool
	needKeyboard bool
	events       []TerminalEvent
	stopping     atomic.Bool
	drainDone    sync.WaitGroup
	beforeWrite  func() // tests: stall Write without blocking the PTY pump
}

func (s *Screen) setBeforeWrite(fn func()) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.beforeWrite = fn
	s.mu.Unlock()
}

func newScreen(cols, rows int) *Screen {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	s := &Screen{sticky: make(map[int]bool)}
	s.emu = s.newEmulator(cols, rows)
	return s
}

func (s *Screen) newEmulator(cols, rows int) *vt.Emulator {
	emu := vt.NewEmulator(cols, rows)
	emu.SetScrollbackSize(0)
	emu.SetCallbacks(vt.Callbacks{
		EnableMode: func(m ansi.Mode) {
			if m != nil {
				s.sticky[m.Mode()] = true
			}
			s.recordMode(TerminalEventModeEnabled, m)
		},
		DisableMode: func(m ansi.Mode) {
			if m != nil {
				delete(s.sticky, m.Mode())
			}
			s.recordMode(TerminalEventModeDisabled, m)
		},
	})
	// vt.Emulator replies to DA/CPR on an unbuffered io.Pipe. Nobody consumes
	// those replies (the session answers the real PTY). Without a drain,
	// emu.Write blocks forever on the first CSI c / CSI 6 n.
	s.stopping.Store(false)
	s.drainDone.Add(1)
	go drainEmulatorInput(s, emu)
	return emu
}

func drainEmulatorInput(s *Screen, emu *vt.Emulator) {
	defer s.drainDone.Done()
	buf := make([]byte, 512)
	for {
		if _, err := emu.Read(buf); err != nil {
			return
		}
		if s.stopping.Load() {
			return
		}
	}
}

func (s *Screen) shutdownEmulator(emu *vt.Emulator) {
	if emu == nil {
		return
	}
	// Unblock drain with a DA reply, then Close. emu.Write can block if the
	// reply pipe is full, so bound the wait; Close then unblocks Read.
	s.stopping.Store(true)
	done := make(chan struct{})
	go func() {
		_, _ = emu.Write([]byte("\x1b[c"))
		s.drainDone.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
	_ = emu.Close()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
	}
}

func (s *Screen) recordMode(kind TerminalEventKind, m ansi.Mode) {
	s.events = append(s.events, TerminalEvent{Kind: kind, Mode: m})
	if !isAltScreenMode(m) {
		return
	}
	switch kind {
	case TerminalEventModeEnabled:
		s.events = append(s.events, TerminalEvent{Kind: TerminalEventAltScreenEntered, Mode: m})
		s.needKeyboard = true
	case TerminalEventModeDisabled:
		s.events = append(s.events, TerminalEvent{Kind: TerminalEventAltScreenLeft, Mode: m})
	}
}

func isAltScreenMode(m ansi.Mode) bool {
	if m == nil {
		return false
	}
	switch m.Mode() {
	case modeAltScreenSaveCursor.Mode(), modeAltScreen.Mode(), modeAltScreenLegacy.Mode():
		return true
	default:
		return false
	}
}

func hasAltScreenLeft(events []TerminalEvent) bool {
	for _, ev := range events {
		if ev.Kind == TerminalEventAltScreenLeft {
			return true
		}
	}
	return false
}

// Write parses p through the shadow emulator and returns every mode transition
// observed in this chunk, including enter+leave of the alternate screen.
func (s *Screen) Write(p []byte) []TerminalEvent {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	hook := s.beforeWrite
	s.mu.Unlock()
	if hook != nil {
		hook()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emu == nil {
		return nil
	}
	s.events = s.events[:0]
	_, _ = s.emu.Write(p)
	out := make([]TerminalEvent, len(s.events))
	copy(out, s.events)
	return out
}

func (s *Screen) IsAltScreen() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emu == nil {
		return false
	}
	return s.emu.IsAltScreen()
}

// idleSnapshot returns leftover sticky DEC modes. forceSticky returns the
// full allowlist even when the emulator never saw those modes (overlay paint).
func (s *Screen) idleSnapshot(forceSticky bool) (leftover []ansi.Mode, onAlt, needKeyboard bool) {
	if s == nil {
		return nil, false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emu == nil {
		return nil, false, false
	}
	onAlt = s.emu.IsAltScreen()
	needKeyboard = s.needKeyboard
	if forceSticky {
		leftover = append([]ansi.Mode(nil), stickyModes...)
		return leftover, onAlt, needKeyboard
	}
	for _, m := range stickyModes {
		if m != nil && s.sticky[m.Mode()] {
			leftover = append(leftover, m)
		}
	}
	return leftover, onAlt, needKeyboard
}

func (s *Screen) clearKeyboardSticky() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.needKeyboard = false
	s.mu.Unlock()
}

func (s *Screen) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	emu := s.emu
	s.emu = nil
	s.mu.Unlock()
	s.shutdownEmulator(emu)
}

func (s *Screen) Reset(cols, rows int) {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	s.mu.Lock()
	old := s.emu
	s.emu = nil
	s.mu.Unlock()
	s.shutdownEmulator(old)
	s.mu.Lock()
	s.sticky = make(map[int]bool)
	s.needKeyboard = false
	s.events = nil
	s.emu = s.newEmulator(cols, rows)
	s.mu.Unlock()
}

func (s *Screen) Resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emu != nil {
		s.emu.Resize(cols, rows)
	}
}

func (s *Screen) Snapshot() ptyproxy.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emu == nil {
		return ptyproxy.Snapshot{}
	}
	return snapshotFrom(s.emu)
}

// Cursor is the emulator cursor in 0-indexed cells.
func (s *Screen) Cursor() (x, y int) {
	if s == nil {
		return 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emu == nil {
		return 0, 0
	}
	pos := s.emu.CursorPosition()
	return pos.X, pos.Y
}

func snapshotFrom(emu *vt.Emulator) ptyproxy.Snapshot {
	w, h := emu.Width(), emu.Height()
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	pos := emu.CursorPosition()
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		rows[y] = encodeRow(emu, y, w)
	}
	return ptyproxy.Snapshot{
		Rows:      h,
		Cols:      w,
		CursorRow: pos.Y,
		CursorCol: pos.X,
		RowANSI:   rows,
	}
}

func encodeRow(emu *vt.Emulator, y, w int) string {
	if w < 1 {
		return ""
	}
	line := make(uv.Line, w)
	for x := 0; x < w; x++ {
		if cell := emu.CellAt(x, y); cell != nil {
			line[x] = *cell
		} else {
			line[x] = uv.EmptyCell
		}
	}
	return line.Render()
}
