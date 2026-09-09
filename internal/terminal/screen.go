//go:build unix

package terminal

import (
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/dont-be-evil-company/remnix/internal/ptyproxy"
)

type Screen struct {
	mu           sync.Mutex
	emu          *vt.Emulator
	sticky       map[int]bool
	needKeyboard bool
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
		},
		DisableMode: func(m ansi.Mode) {
			if m != nil {
				delete(s.sticky, m.Mode())
			}
		},
	})
	// vt.Emulator replies to DA/CPR on an unbuffered io.Pipe. Nobody consumes
	// those replies (queryScanner answers the real PTY). Without a drain,
	// emu.Write blocks forever on the first CSI c / CSI 6 n - nvim's handshake.
	go drainEmulatorInput(emu)
	return emu
}

func drainEmulatorInput(emu *vt.Emulator) {
	buf := make([]byte, 512)
	for {
		if _, err := emu.Read(buf); err != nil {
			return
		}
	}
}

func (s *Screen) Write(p []byte) (entered, left bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emu == nil {
		return false, false
	}
	was := s.emu.IsAltScreen()
	_, _ = s.emu.Write(p)
	now := s.emu.IsAltScreen()
	entered, left = !was && now, was && !now
	if entered {
		s.needKeyboard = true
	}
	return entered, left
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
	if emu != nil {
		_ = emu.Close()
	}
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
	s.sticky = make(map[int]bool)
	s.needKeyboard = false
	s.emu = s.newEmulator(cols, rows)
	s.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
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
