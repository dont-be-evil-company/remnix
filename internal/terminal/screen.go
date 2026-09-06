//go:build unix

package terminal

import (
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/mistweaverco/syncsh/internal/ptyproxy"
)

type Screen struct {
	mu  sync.Mutex
	emu *vt.Emulator
}

func newScreen(cols, rows int) *Screen {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	emu := vt.NewEmulator(cols, rows)
	emu.SetScrollbackSize(0)
	return &Screen{emu: emu}
}

func (s *Screen) Write(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.emu.Write(p)
}

func (s *Screen) Resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emu.Resize(cols, rows)
}

func (s *Screen) Snapshot() ptyproxy.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return snapshotFrom(s.emu)
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
	b := make([]byte, 0, w+8)
	styled := false
	havePrev := false
	var prevStyle uv.Style
	for x := 0; x < w; {
		cell := emu.CellAt(x, y)
		if cell == nil || cell.Width == 0 {
			if styled {
				b = append(b, "\x1b[0m"...)
				styled = false
				havePrev = false
			}
			b = append(b, ' ')
			x++
			continue
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		st := cell.Style
		if st.IsZero() {
			if styled {
				b = append(b, "\x1b[0m"...)
				styled = false
				havePrev = false
			}
			b = append(b, content...)
		} else {
			if !havePrev {
				b = append(b, st.String()...)
			} else if !prevStyle.Equal(&st) {
				b = append(b, st.Diff(&prevStyle)...)
			}
			prevStyle = st
			havePrev = true
			styled = true
			b = append(b, content...)
		}
		if cell.Width > 1 {
			x += cell.Width
		} else {
			x++
		}
	}
	if styled {
		b = append(b, "\x1b[0m"...)
	}
	return string(b)
}
