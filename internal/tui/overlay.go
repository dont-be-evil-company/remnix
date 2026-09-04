package tui

import (
	"io"
	"os"
	"strings"

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
//   - stdout must be a TTY (fd-swap in the shell widget). Otherwise Atuin
//     forces fullscreen because cursor queries fail on a pipe.
//   - height < terminal → popup: fetch snapshot, Place, scroll/clear/CUP.
//   - height >= terminal or no snapshot → nil (caller uses alt-screen).
func beginOverlay(w *os.File, stdoutIsTTY bool, rowsFor func(termRows int) int) *overlaySession {
	if !stdoutIsTTY || w == nil {
		return nil
	}
	snap, err := ptyproxy.Fetch()
	if err != nil {
		return nil
	}
	height := snap.Rows
	if rowsFor != nil {
		height = rowsFor(snap.Rows)
	}
	if height < 1 || height >= snap.Rows {
		return nil
	}
	place := ptyproxy.Place(snap.CursorRow, snap.Rows, snap.Cols, height)
	ptyproxy.Prepare(w, snap, place)
	return &overlaySession{snap: snap, place: place}
}

func (s *overlaySession) end(w *os.File) {
	if w == nil {
		return
	}
	if s != nil {
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

// widgetIO matches Atuin TerminalWriter: TUI on stdout when it is a TTY,
// otherwise /dev/tty (and the caller must use fullscreen, not overlay).
func widgetIO() (in, out *os.File, closeFn func(), stdoutIsTTY bool, err error) {
	if isTerminalFile(os.Stdout) {
		if isTerminalFile(os.Stdin) {
			return os.Stdin, os.Stdout, func() {}, true, nil
		}
		inTTY, _, closeIn, openErr := OpenTTY()
		if openErr != nil {
			return os.Stdin, os.Stdout, func() {}, true, nil
		}
		return inTTY, os.Stdout, closeIn, true, nil
	}
	in, out, closeFn, err = OpenTTY()
	return in, out, closeFn, false, err
}

func isTerminalFile(f *os.File) bool {
	return f != nil && term.IsTerminal(int(f.Fd()))
}

func widgetProgram(m tea.Model, rowsFor func(termRows int) int) (*tea.Program, func(), error) {
	prepareWidgetTTY()
	in, outTTY, closeFn, stdoutIsTTY, err := widgetIO()
	if err != nil {
		return tea.NewProgram(m), func() {}, nil
	}
	opts := []tea.ProgramOption{tea.WithInput(in)}
	sess := beginOverlay(outTTY, stdoutIsTTY, rowsFor)
	if sess != nil {
		st := sess.state(outTTY)
		// Bubble Tea has no Viewport::Fixed. Its inline renderer uses relative
		// cursor and would fight Atuin-style CUPs. Render to Discard; we paint
		// the popup ourselves. WithWindowSize sticks because Discard is not a TTY
		// (GetSize would otherwise overwrite it with the full terminal).
		opts = append(opts,
			tea.WithOutput(io.Discard),
			tea.WithWindowSize(st.TermCols, st.TermRows),
			tea.WithColorProfile(colorprofile.Detect(outTTY, os.Environ())),
		)
		if om, ok := m.(overlayAware); ok {
			om.EnableOverlay(st)
			m = om.(tea.Model)
		}
	} else {
		opts = append(opts, tea.WithOutput(outTTY))
	}
	cleanup := func() {
		sess.end(outTTY)
		closeFn()
	}
	return tea.NewProgram(m, opts...), cleanup, nil
}

type overlayAware interface {
	EnableOverlay(OverlayState)
}
