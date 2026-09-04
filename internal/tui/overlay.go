package tui

import (
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"

	"github.com/mistweaverco/syncsh/internal/ptyproxy"
)

// Search overlay chrome: header, rule, help, input. The list needs at least
// SearchListMinRows rows on top of that.
const (
	SearchListMinRows      = 5
	SearchChromeRows       = 4
	SearchMinOverlayHeight = SearchChromeRows + SearchListMinRows
)

// OverlayState is the geometry for a pty-proxy popup. Matches Atuin's
// Viewport::Fixed: we CUP to this rect every frame. Out is the TTY; Bubble
// Tea's renderer is pointed at io.Discard so it cannot fight those CUPs.
type OverlayState struct {
	TermCols, TermRows int
	RectY, RectH       int
	CursorRow          int
	Out                io.Writer
}

type overlaySession struct {
	snap  ptyproxy.Snapshot
	place ptyproxy.Placement
	rows  <-chan []string
}

func overlayRows(termRows, percent int) int {
	if termRows < 1 {
		termRows = 1
	}
	if percent <= 0 || percent >= 100 {
		return termRows
	}
	h := (termRows*percent + 50) / 100
	minH := SearchMinOverlayHeight
	if minH > termRows {
		minH = termRows
	}
	if h < minH {
		h = minH
	}
	if h > termRows {
		h = termRows
	}
	return h
}

func overlayOriginY(cursorRow, termRows, termCols, height int) int {
	if height >= termRows {
		return 0
	}
	return ptyproxy.Place(cursorRow, termRows, termCols, height).Rect.Y
}

// composeOverlay embeds body in a full-terminal canvas. Tests use it to lock
// the pin-body geometry; the live widget does not composite (Atuin Fixed
// viewport paints only the popup rect).
func composeOverlay(termRows, y, h int, bg []string, body string) string {
	if termRows < 1 {
		return body
	}
	if h < 1 || h > termRows {
		h = termRows
	}
	if y < 0 {
		y = 0
	}
	if y+h > termRows {
		y = termRows - h
	}
	if y <= 0 && h >= termRows {
		return body
	}
	lines := strings.Split(body, "\n")
	out := make([]string, termRows)
	for i := 0; i < termRows; i++ {
		if i >= y && i < y+h {
			j := i - y
			if j >= 0 && j < len(lines) {
				out[i] = lines[j]
			}
			continue
		}
		if i < len(bg) {
			out[i] = bg[i]
		}
	}
	return strings.Join(out, "\n")
}

// beginOverlay follows Atuin interactive.rs history():
//
//   - the paint writer must be a TTY (/dev/tty, even if os.Stdout is a pipe).
//   - height < terminal → popup. Geometry comes from pty-proxy when the
//     socket answers; otherwise GetSize on the paint TTY so fish/nu still
//     get a 40% band instead of alt-screen.
//     Row ANSI is read in the background so a full-screen greeting cannot
//     stall the first paint for the snapshot deadline.
//   - height >= terminal → nil (caller uses alt-screen).
func beginOverlay(w *os.File, rowsFor func(termRows int) int) *overlaySession {
	if w == nil || !isTerminalFile(w) {
		return nil
	}
	snap, rest, err := overlaySnapshot(w)
	if err != nil {
		return nil
	}
	height := snap.Rows
	if rowsFor != nil {
		height = rowsFor(snap.Rows)
	}
	if height < 1 || height >= snap.Rows {
		if rest != nil {
			_ = rest.Close()
		}
		return nil
	}
	place := ptyproxy.Place(snap.CursorRow, snap.Rows, snap.Cols, height)
	ptyproxy.Prepare(w, snap, place)
	ch := make(chan []string, 1)
	if rest != nil {
		go func() {
			defer rest.Close()
			if c, ok := rest.(interface{ SetDeadline(time.Time) error }); ok {
				_ = c.SetDeadline(time.Now().Add(500 * time.Millisecond))
			}
			rows, err := ptyproxy.ReadRows(rest, snap.Rows)
			if err != nil {
				return
			}
			ch <- rows
		}()
	}
	return &overlaySession{snap: snap, place: place, rows: ch}
}

// overlaySnapshot prefers the pty-proxy screen (cursor + restore rows). If
// that is missing, fall back to the paint TTY's size so a 40% popup still
// works in fish/nu when stdout is a pipe and the snapshot socket is late.
func overlaySnapshot(w *os.File) (ptyproxy.Snapshot, io.ReadCloser, error) {
	snap, rest, err := ptyproxy.FetchGeom()
	if err == nil && snap.Rows >= 2 && snap.Cols >= 1 {
		return snap, rest, nil
	}
	if rest != nil {
		_ = rest.Close()
	}
	cols, rows, gerr := term.GetSize(int(w.Fd()))
	if gerr != nil || rows < 2 || cols < 1 {
		if err != nil {
			return ptyproxy.Snapshot{}, nil, err
		}
		if gerr != nil {
			return ptyproxy.Snapshot{}, nil, gerr
		}
		return ptyproxy.Snapshot{}, nil, ptyproxy.ErrNoProxy
	}
	return ptyproxy.Snapshot{
		Rows:      rows,
		Cols:      cols,
		CursorRow: rows - 1,
	}, nil, nil
}

func (s *overlaySession) end(w *os.File) {
	if w == nil {
		return
	}
	if s != nil {
		select {
		case rows := <-s.rows:
			s.snap.RowANSI = rows
		default:
		}
		s.snap.Restore(w, s.place.Rect, s.place.Scroll)
	}
	_, _ = io.WriteString(w, "\x1b[?25h")
}

func (s *overlaySession) state(out io.Writer) OverlayState {
	if s == nil {
		return OverlayState{}
	}
	return OverlayState{
		TermCols:  s.snap.Cols,
		TermRows:  s.snap.Rows,
		RectY:     s.place.Rect.Y,
		RectH:     s.place.Rect.H,
		CursorRow: s.snap.CursorRow,
		Out:       out,
	}
}

// drawFixed paints body at (0, y) for h rows. This is ratatui Viewport::Fixed:
// absolute CUP every frame, no alt-screen, no relative cursor.
func drawFixed(w io.Writer, cols, y, h int, body string) {
	if w == nil || h < 1 {
		return
	}
	if cols < 1 {
		cols = 1
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	_, _ = io.WriteString(w, "\x1b[?25l")
	for i := 0; i < h; i++ {
		ptyproxy.MoveTo(w, 0, y+i)
		_, _ = io.WriteString(w, "\x1b[0m")
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		if lipgloss.Width(line) > cols {
			line = ansi.Truncate(line, cols, "")
		}
		_, _ = io.WriteString(w, line)
		if pad := cols - lipgloss.Width(line); pad > 0 {
			_, _ = io.WriteString(w, strings.Repeat(" ", pad))
		}
	}
	if f, ok := w.(interface{ Sync() error }); ok {
		_ = f.Sync()
	}
}

// widgetIO prefers /dev/tty for painting so fish bind and nu executehostcommand
// can overlay even when os.Stdout is a pipe. Input stays on stdin when that
// is already a TTY (zsh fd-swap).
func widgetIO() (in, out *os.File, closeFn func(), err error) {
	in, out, closeFn, err = OpenTTY()
	if err != nil {
		return os.Stdin, os.Stdout, func() {}, nil
	}
	if isTerminalFile(os.Stdin) {
		in = os.Stdin
	}
	return in, out, closeFn, nil
}

func isTerminalFile(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}

func widgetProgram(m tea.Model, rowsFor func(termRows int) int) (*tea.Program, func(), error) {
	prepareWidgetTTY()
	in, outTTY, closeFn, err := widgetIO()
	if err != nil {
		return tea.NewProgram(m), func() {}, nil
	}
	opts := []tea.ProgramOption{tea.WithInput(in)}
	sess := beginOverlay(outTTY, rowsFor)
	if sess != nil {
		st := sess.state(outTTY)
		// Bubble Tea has no Viewport::Fixed. Its inline renderer uses relative
		// cursor and would fight Atuin-style CUPs. Render to Discard; we paint
		// the popup ourselves. WithWindowSize sticks because Discard is not a TTY
		// (GetSize would otherwise overwrite it with the full terminal).
		opts = append(opts,
			tea.WithOutput(io.Discard),
			tea.WithWindowSize(st.TermCols, st.TermRows),
			tea.WithColorProfile(colorprofile.Env(os.Environ())),
		)
		if om, ok := m.(overlayAware); ok {
			om.EnableOverlay(st)
			m = om.(tea.Model)
		}
	} else {
		opts = append(opts, tea.WithOutput(outTTY))
	}
	// After snapshot: fish 4 leaves kitty keyboard / modifyOtherKeys on
	// during command substitution (no tty handoff). Overlay output is
	// Discard, so Bubble Tea never writes the disable sequences.
	resumeKB := suspendShellKeyboard(outTTY)
	cleanup := func() {
		sess.end(outTTY)
		resumeKB()
		closeFn()
	}
	return tea.NewProgram(m, opts...), cleanup, nil
}

// suspendShellKeyboard pushes a kitty-protocol stack entry with no flags so
// the widget sees plain keys (and Escape). Pop on restore so the parent
// shell (fish 4 especially) keeps the flags it SET before the widget.
func suspendShellKeyboard(w io.Writer) func() {
	if w == nil {
		return func() {}
	}
	_, _ = io.WriteString(w, ansi.DisableKittyKeyboard)
	_, _ = io.WriteString(w, "\x1b[>4;0m")
	return func() {
		_, _ = io.WriteString(w, ansi.PopKittyKeyboard(1))
		_, _ = io.WriteString(w, ansi.SetModifyOtherKeys1)
	}
}

type overlayAware interface {
	EnableOverlay(OverlayState)
}
