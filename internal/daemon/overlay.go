package daemon

import (
	"context"
	"fmt"
	"io"

	"github.com/dont-be-evil-company/remnix/internal/history"
	"github.com/dont-be-evil-company/remnix/internal/ptyproxy"
	"github.com/dont-be-evil-company/remnix/internal/terminal"
	"github.com/dont-be-evil-company/remnix/internal/tui"
)

func (s *Server) overlayPercent() int {
	if s.app != nil && s.app.Config != nil {
		return s.app.Config.PtyProxy.HeightPercent()
	}
	return 100
}

func (s *Server) searchInteractive(query, cwd, sessionID string) (string, error) {
	if s.sessions == nil {
		return "", terminal.ErrNoSession
	}
	if s.sessions.Get(sessionID) == nil {
		return "", fmt.Errorf("%w %q", terminal.ErrNoSession, sessionID)
	}
	cands := []history.Entry{}
	if s.history != nil {
		var err error
		cands, err = s.history.SearchCandidates(query, cwd, "", false, 5000)
		if err != nil {
			return "", err
		}
	}
	percent := s.overlayPercent()
	deviceID := s.deviceID()
	return s.sessions.RunOverlay(sessionID, func(termRows int) int {
		return tui.OverlayRows(termRows, percent)
	}, func(keys io.Reader, paint io.Writer, snap ptyproxy.Snapshot, place ptyproxy.Placement, winch <-chan terminal.Size, ctx context.Context) (string, error) {
		cmd, run, err := tui.RunSearchOn(cands, tui.Options{
			Query:          query,
			Cwd:            cwd,
			DeviceID:       deviceID,
			OverlayPercent: percent,
			Delete: func(e history.Entry) error {
				if s.history == nil {
					return nil
				}
				return s.history.TombstoneCommand(e.Command)
			},
		}, tuiRemote(keys, paint, snap, place, winch, ctx))
		if err != nil {
			return "", err
		}
		if cmd == "" {
			return "", nil
		}
		return tui.FormatSelection(cmd, run), nil
	})
}

func (s *Server) suggestInteractive(prefix, cwd, sessionID string) (string, error) {
	if s.sessions == nil {
		return "", terminal.ErrNoSession
	}
	if s.sessions.Get(sessionID) == nil {
		return "", fmt.Errorf("%w %q", terminal.ErrNoSession, sessionID)
	}
	limit := 8
	typed, hist := "›", "*"
	if s.app != nil && s.app.Config != nil {
		limit = s.app.Config.Suggest.MenuLimit()
		typed = s.app.Config.IconTyped()
		hist = s.app.Config.IconHistory()
	}
	var items []string
	if s.history != nil {
		cands, err := s.history.SuggestCandidates(prefix, cwd)
		if err != nil {
			return "", err
		}
		items = s.suggestList(prefix, cwd, cands, limit)
	}
	return s.runSuggestOverlay(prefix, sessionID, items, nil, typed, hist, limit, nil)
}

func (s *Server) suggestCompleteInteractive(prefix, cwd, sessionID string, waitItems func() (items, descrs []string, abort bool, err error)) (string, error) {
	if s.sessions == nil {
		if _, _, _, err := waitItems(); err != nil {
			return "", err
		}
		return "", terminal.ErrNoSession
	}
	if s.sessions.Get(sessionID) == nil {
		_, _, _, err := waitItems()
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("%w %q", terminal.ErrNoSession, sessionID)
	}
	limit := 8
	typed, itemIco := "›", "+"
	if s.app != nil && s.app.Config != nil {
		limit = s.app.Config.Suggest.MenuLimit()
		typed = s.app.Config.IconTyped()
		itemIco = s.app.Config.IconCompletion()
	}
	itemsCh := make(chan tui.SuggestItems, 1)
	type overlayResult struct {
		sel string
		err error
	}
	done := make(chan overlayResult, 1)
	go func() {
		sel, err := s.runSuggestOverlay(prefix, sessionID, nil, nil, typed, itemIco, limit, itemsCh)
		done <- overlayResult{sel: sel, err: err}
	}()
	items, descrs, abort, err := waitItems()
	if err != nil {
		select {
		case itemsCh <- tui.SuggestItems{Abort: true}:
			res := <-done
			if res.err != nil {
				return "", res.err
			}
			return "", err
		case res := <-done:
			if res.err != nil {
				return "", res.err
			}
			return "", err
		}
	}
	if abort {
		select {
		case itemsCh <- tui.SuggestItems{Abort: true}:
		case res := <-done:
			return res.sel, res.err
		}
		res := <-done
		return res.sel, res.err
	}
	if len(items) == 0 && s.history != nil {
		cands, histErr := s.history.SuggestCandidates(prefix, cwd)
		if histErr == nil {
			items = s.suggestList(prefix, cwd, cands, limit)
			descrs = nil
		}
	}
	select {
	case itemsCh <- tui.SuggestItems{Items: items, Descrs: descrs}:
		res := <-done
		return res.sel, res.err
	case res := <-done:
		return res.sel, res.err
	}
}

func (s *Server) runSuggestOverlay(prefix, sessionID string, items, descrs []string, typed, itemIco string, limit int, wait <-chan tui.SuggestItems) (string, error) {
	h := limit + 3
	n := len(items)
	if wait != nil {
		n = limit
	}
	return s.sessions.RunOverlay(sessionID, func(termRows int) int {
		return tui.SuggestOverlayRows(termRows, h, n)
	}, func(keys io.Reader, paint io.Writer, snap ptyproxy.Snapshot, place ptyproxy.Placement, winch <-chan terminal.Size, ctx context.Context) (string, error) {
		cmd, run, err := tui.RunSuggestOn(tui.SuggestMenuOptions{
			Prefix:        prefix,
			Items:         items,
			Descrs:        descrs,
			TypedIcon:     typed,
			HistoryIcon:   itemIco,
			ItemIcon:      itemIco,
			OverlayHeight: h,
			Widget:        true,
			ItemsCh:       wait,
		}, tuiRemote(keys, paint, snap, place, winch, ctx))
		if err != nil {
			return "", err
		}
		if cmd == "" {
			return "", nil
		}
		return tui.FormatSelection(cmd, run), nil
	})
}

func tuiRemote(keys io.Reader, paint io.Writer, snap ptyproxy.Snapshot, place ptyproxy.Placement, winch <-chan terminal.Size, ctx context.Context) tui.Remote {
	var out <-chan tui.Size
	if winch != nil {
		ch := make(chan tui.Size, 1)
		go func() {
			defer close(ch)
			for {
				select {
				case <-ctx.Done():
					return
				case sz, ok := <-winch:
					if !ok {
						return
					}
					select {
					case ch <- tui.Size{Cols: sz.Cols, Rows: sz.Rows}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
		out = ch
	}
	return tui.Remote{
		Keys:  keys,
		Paint: paint,
		Snap:  snap,
		Place: place,
		Winch: out,
		Ctx:   ctx,
	}
}
