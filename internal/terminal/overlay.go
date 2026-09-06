package terminal

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/mistweaverco/syncsh/internal/ptyproxy"
)

var (
	ErrOverlayBusy = errors.New("overlay already active")
	ErrNoSession   = errors.New("unknown terminal session")
)

// Size is a terminal size in cells.
type Size struct {
	Cols, Rows int
}

// Overlay is a daemon-hosted TUI painted on the attach stream. Keys from
// attach are diverted away from the inner PTY until Close.
type Overlay struct {
	Keys  io.Reader
	Paint io.Writer
	Snap  ptyproxy.Snapshot
	Place ptyproxy.Placement
	Winch <-chan Size
	Ctx   context.Context

	closeFn func()
}

func (o *Overlay) Close() {
	if o != nil && o.closeFn != nil {
		o.closeFn()
		o.closeFn = nil
	}
}

// OverlayRun is the TUI body. It must return when Ctx is cancelled.
type OverlayRun func(keys io.Reader, paint io.Writer, snap ptyproxy.Snapshot, place ptyproxy.Placement, winch <-chan Size, ctx context.Context) (string, error)

func (m *Manager) RunOverlay(id string, rowsFor func(termRows int) int, run OverlayRun) (string, error) {
	if m == nil {
		return "", ErrNoSession
	}
	s := m.Get(id)
	if s == nil {
		return "", fmt.Errorf("%w %q", ErrNoSession, id)
	}
	ov, err := s.BeginOverlay(rowsFor)
	if err != nil {
		return "", err
	}
	defer ov.Close()
	return run(ov.Keys, ov.Paint, ov.Snap, ov.Place, ov.Winch, ov.Ctx)
}
