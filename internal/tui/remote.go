package tui

import (
	"context"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/dont-be-evil-company/remnix/internal/history"
	"github.com/dont-be-evil-company/remnix/internal/ptyproxy"
)

// Size is a terminal size in cells, used for WINCH while a remote overlay runs.
type Size struct {
	Cols, Rows int
}

// Remote is a TTY-less overlay: keys and paint go over the daemon attach stream.
type Remote struct {
	Keys  io.Reader
	Paint io.Writer
	Snap  ptyproxy.Snapshot
	Place ptyproxy.Placement
	Winch <-chan Size
	Ctx   context.Context
}

func overlayStateFrom(r Remote) OverlayState {
	h := r.Place.Rect.H
	if h < 1 {
		h = r.Snap.Rows
	}
	y := r.Place.Rect.Y
	if y < 0 {
		y = 0
	}
	return OverlayState{
		TermCols:  r.Snap.Cols,
		TermRows:  r.Snap.Rows,
		RectY:     y,
		RectH:     h,
		CursorRow: r.Snap.CursorRow,
		Out:       r.Paint,
	}
}

func runRemote(m tea.Model, r Remote) (tea.Model, error) {
	st := overlayStateFrom(r)
	if om, ok := m.(overlayAware); ok {
		om.EnableOverlay(st)
		m = om.(tea.Model)
	}
	ctx := r.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	opts := []tea.ProgramOption{
		tea.WithInput(r.Keys),
		tea.WithOutput(io.Discard),
		tea.WithWindowSize(max(1, st.TermCols), max(1, st.TermRows)),
		tea.WithColorProfile(colorprofile.Env(os.Environ())),
		tea.WithContext(ctx),
		tea.WithoutSignalHandler(),
	}
	p := tea.NewProgram(m, opts...)
	if r.Winch != nil {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case sz, ok := <-r.Winch:
					if !ok {
						return
					}
					p.Send(tea.WindowSizeMsg{Width: max(1, sz.Cols), Height: max(1, sz.Rows)})
				}
			}
		}()
	}
	// Paint once before Run. If tea blocks on the key pipe, the popup is
	// already on the attach stream instead of a lone blinking cursor.
	if vm, ok := m.(interface{ View() tea.View }); ok {
		_ = vm.View()
	}
	if f, ok := r.Paint.(interface{ Sync() error }); ok {
		_ = f.Sync()
	} else if f, ok := r.Paint.(interface{ Flush() error }); ok {
		_ = f.Flush()
	}
	return p.Run()
}

func selectionFrom(final tea.Model) (cmd string, run bool, ok bool) {
	switch got := final.(type) {
	case model:
		cmd, ok = got.SelectedCommand()
		return cmd, got.RunSelected(), ok
	case *model:
		if got == nil {
			return "", false, false
		}
		cmd, ok = got.SelectedCommand()
		return cmd, got.RunSelected(), ok
	case suggestModel:
		cmd, ok = got.SelectedCommand()
		return cmd, got.RunSelected(), ok
	case *suggestModel:
		if got == nil {
			return "", false, false
		}
		cmd, ok = got.SelectedCommand()
		return cmd, got.RunSelected(), ok
	}
	return "", false, false
}

// RunSearchOn runs the Ctrl+R widget against an attach stream.
func RunSearchOn(entries []history.Entry, opts Options, r Remote) (cmd string, run bool, err error) {
	m := New(entries, opts)
	final, err := runRemote(&m, r)
	if err != nil {
		return "", false, err
	}
	cmd, run, _ = selectionFrom(final)
	return cmd, run, nil
}

// RunSuggestOn runs the Ctrl+Space suggest overlay against an attach stream.
func RunSuggestOn(opts SuggestMenuOptions, r Remote) (cmd string, run bool, err error) {
	m := newSuggestModel(opts)
	final, err := runRemote(&m, r)
	if err != nil {
		return "", false, err
	}
	cmd, run, _ = selectionFrom(final)
	switch got := final.(type) {
	case suggestModel:
		if got.ContinueSelected() {
			return ContinuePrefix + cmd, false, nil
		}
	case *suggestModel:
		if got != nil && got.ContinueSelected() {
			return ContinuePrefix + cmd, false, nil
		}
	}
	return cmd, run, nil
}

// SuggestOverlayRows is the popup height for the suggest menu.
func SuggestOverlayRows(termRows, overlayHeight, nItems int) int {
	h := overlayHeight
	if h <= 0 {
		h = max(SearchChromeRows+1, min(nItems+3, 12))
	}
	if h > termRows {
		return termRows
	}
	if h < 1 {
		return 1
	}
	return h
}
