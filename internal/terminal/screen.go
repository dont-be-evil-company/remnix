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
	beforeWrite  func() // tests: stall Write without blocking the PTY pump
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
	if s == nil {
		return false, false
	}
	if hook := s.beforeWrite; hook != nil {
		hook()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.emu == nil {
		return false, false
	}
	was := s.emu.IsAltScreen()
	_, _ = s.emu.Write(p)
	now := s.emu.IsAltScreen()
	entered, left = !was && now, was && !now
	// One coalesced nvim burst can contain both 1049h and 1049l. Net emulator
	// state is unchanged, but the TUI still left and the shell needs idle reset.
	sawEnter, sawLeave := altScreenHops(p)
	if !entered && !left && sawEnter && sawLeave {
		entered, left = true, true
	}
	if entered {
		s.needKeyboard = true
	}
	return entered, left
}

func altScreenHops(p []byte) (enter, leave bool) {
	i := 0
	for i < len(p) {
		if p[i] != 0x1b {
			i++
			continue
		}
		if i+1 >= len(p) || p[i+1] != '[' {
			i++
			continue
		}
		j := i + 2
		for j < len(p) && !csiFinal(p[j]) {
			j++
		}
		if j >= len(p) {
			return enter, leave
		}
		seq := p[i : j+1]
		if hopEnter, hopLeave := altScreenCSI(seq); hopEnter || hopLeave {
			enter = enter || hopEnter
			leave = leave || hopLeave
		}
		i = j + 1
	}
	return enter, leave
}

func altScreenCSI(seq []byte) (enter, leave bool) {
	n := len(seq)
	if n < 5 || seq[0] != 0x1b || seq[1] != '[' || seq[2] != '?' {
		return false, false
	}
	fin := seq[n-1]
	if fin != 'h' && fin != 'l' {
		return false, false
	}
	body := string(seq[3 : n-1])
	start := 0
	for start <= len(body) {
		end := start
		for end < len(body) && body[end] != ';' {
			end++
		}
		mode := body[start:end]
		if mode == "1049" || mode == "1047" || mode == "47" {
			if fin == 'h' {
				enter = true
			} else {
				leave = true
			}
		}
		if end >= len(body) {
			break
		}
		start = end + 1
	}
	return enter, leave
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
