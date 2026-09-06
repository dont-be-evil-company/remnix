//go:build unix

package terminal

import (
	"context"
	"io"
	"sync"

	"github.com/mistweaverco/syncsh/internal/ptyproxy"
)

const (
	seqDisableKitty = "\x1b[>u"
	seqModifyOff    = "\x1b[>4;0m"
	seqFocusOff     = "\x1b[?1004l"
	seqPopKitty     = "\x1b[<1u"
	seqModifyOn     = "\x1b[>4;1m"
	// seqUnstick ends OSC/DCS that attach may still be holding after nvim,
	// leaves alt-screen / synchronized output / mouse / paste, then SGR.
	seqUnstick = "\x1b\\\x07\x1b[?2026l\x1b[?1049l\x1b[?2004l\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?25l\x1b[0m"
)

type frameWriter struct {
	s   *Session
	mu  sync.Mutex
	buf []byte
}

func (w *frameWriter) Write(p []byte) (int, error) {
	if w == nil || len(p) == 0 {
		return len(p), nil
	}
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	w.mu.Unlock()
	return len(p), nil
}

func (w *frameWriter) Flush() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushLocked()
}

func (w *frameWriter) Sync() error { return w.Flush() }

func (w *frameWriter) flushLocked() error {
	if len(w.buf) == 0 {
		return nil
	}
	payload := append([]byte(nil), w.buf...)
	w.buf = w.buf[:0]
	return w.s.sendFrame(FrameData, payload)
}

func (s *Session) BeginOverlay(rowsFor func(termRows int) int) (*Overlay, error) {
	if s == nil {
		return nil, ErrNoSession
	}
	snap := s.Snapshot()
	if snap.Rows < 4 {
		snap.Rows = s.Rows
	}
	if snap.Cols < 8 {
		snap.Cols = s.Cols
	}
	if snap.Rows < 4 {
		snap.Rows = 24
	}
	if snap.Cols < 8 {
		snap.Cols = 80
	}
	if snap.CursorRow < 0 || snap.CursorRow >= snap.Rows {
		snap.CursorRow = snap.Rows - 1
	}
	height := snap.Rows
	if rowsFor != nil {
		height = rowsFor(snap.Rows)
	}
	if height < 1 {
		height = snap.Rows
	}
	if height > snap.Rows {
		height = snap.Rows
	}
	place := ptyproxy.Place(snap.CursorRow, snap.Rows, snap.Cols, height)

	s.overlayMu.Lock()
	if s.overlayActive.Load() {
		s.overlayMu.Unlock()
		return nil, ErrOverlayBusy
	}
	keys := make(chan []byte, 256)
	winch := make(chan Size, 1)
	ctx, cancel := context.WithCancel(context.Background())
	s.overlayKeys = keys
	s.overlayWinch = winch
	s.overlayCancel = cancel
	s.overlayActive.Store(true)
	s.overlayMu.Unlock()

	keyR, keyW := io.Pipe()
	go func() {
		defer keyW.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.closed:
				return
			case b, ok := <-keys:
				if !ok {
					return
				}
				if _, err := keyW.Write(b); err != nil {
					return
				}
			}
		}
	}()
	go func() {
		select {
		case <-s.closed:
			cancel()
		case <-ctx.Done():
		}
	}()

	paint := &frameWriter{s: s}
	_, _ = io.WriteString(paint, seqUnstick)
	_ = paint.Flush()
	ptyproxy.Prepare(paint, snap, place)
	_, _ = io.WriteString(paint, seqDisableKitty)
	_, _ = io.WriteString(paint, seqModifyOff)
	_, _ = io.WriteString(paint, seqFocusOff)
	_ = paint.Flush()

	ov := &Overlay{
		Keys:  keyR,
		Paint: paint,
		Snap:  snap,
		Place: place,
		Winch: winch,
		Ctx:   ctx,
	}
	var once sync.Once
	ov.closeFn = func() {
		once.Do(func() {
			snap.Restore(paint, place.Rect, place.Scroll)
			_, _ = io.WriteString(paint, seqPopKitty)
			_, _ = io.WriteString(paint, seqModifyOn)
			_ = paint.Flush()
			s.endOverlay(cancel, keys, winch)
			_ = keyR.Close()
		})
	}
	return ov, nil
}

func (s *Session) endOverlay(cancel context.CancelFunc, keys chan []byte, winch chan Size) {
	s.overlayMu.Lock()
	if s.overlayKeys == keys {
		s.overlayKeys = nil
	}
	if s.overlayWinch == winch {
		s.overlayWinch = nil
	}
	if s.overlayCancel != nil {
		s.overlayCancel = nil
	}
	s.overlayActive.Store(false)
	s.overlayMu.Unlock()
	cancel()
}

func (s *Session) sendOverlayKey(p []byte) {
	if !s.overlayActive.Load() {
		return
	}
	s.overlayMu.Lock()
	ch := s.overlayKeys
	s.overlayMu.Unlock()
	if ch == nil {
		return
	}
	cp := append([]byte(nil), p...)
	select {
	case ch <- cp:
	default:
	}
}

func (s *Session) sendOverlayWinch(cols, rows int) {
	if !s.overlayActive.Load() {
		return
	}
	s.overlayMu.Lock()
	ch := s.overlayWinch
	s.overlayMu.Unlock()
	if ch == nil {
		return
	}
	sz := Size{Cols: cols, Rows: rows}
	select {
	case ch <- sz:
	default:
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- sz:
		default:
		}
	}
}

func (s *Session) cancelOverlay() {
	s.overlayMu.Lock()
	cancel := s.overlayCancel
	s.overlayCancel = nil
	s.overlayActive.Store(false)
	s.overlayMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
