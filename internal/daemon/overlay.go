package daemon

import (
	"context"
	"fmt"
	"io"

	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/ptyproxy"
	"github.com/mistweaverco/syncsh/internal/terminal"
	"github.com/mistweaverco/syncsh/internal/tui"
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
	h := limit + 3
	return s.sessions.RunOverlay(sessionID, func(termRows int) int {
		return tui.SuggestOverlayRows(termRows, h, len(items))
	}, func(keys io.Reader, paint io.Writer, snap ptyproxy.Snapshot, place ptyproxy.Placement, winch <-chan terminal.Size, ctx context.Context) (string, error) {
		cmd, run, err := tui.RunSuggestOn(tui.SuggestMenuOptions{
			Prefix:        prefix,
			Items:         items,
			TypedIcon:     typed,
			HistoryIcon:   hist,
			OverlayHeight: h,
			Widget:        true,
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
