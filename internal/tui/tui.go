package tui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/search"
)

type Options struct {
	Query  string
	Cwd    string
	Widget bool
	Delete func(history.Entry) error
}

type model struct {
	input    textinput.Model
	all      []history.Entry
	visible  []history.Entry
	cursor   int
	cwd      string
	cwdOnly  bool
	selected string
	print    bool
	run      bool
	quitting bool
	width    int
	height   int
	delete   func(history.Entry) error
	status   string
}

var (
	colDuration = lipgloss.Color("#A6E3A1")
	colFailed   = lipgloss.Color("#F38BA8")
	colTime     = lipgloss.Color("#7F849C")
	colAccent   = lipgloss.Color("#F5C2E7")
	colMuted    = lipgloss.Color("#585B70")
	colTitle    = lipgloss.Color("#CBA6F7")
	colBadge    = lipgloss.Color("#89B4FA")
	colRule     = lipgloss.Color("#313244")
)

var (
	styleTitle    = lipgloss.NewStyle().Foreground(colTitle).Bold(true)
	styleMuted    = lipgloss.NewStyle().Foreground(colMuted)
	styleAccent   = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styleDuration = lipgloss.NewStyle().Foreground(colDuration)
	styleFailed   = lipgloss.NewStyle().Foreground(colFailed)
	styleTime     = lipgloss.NewStyle().Foreground(colTime)
	styleBadge    = lipgloss.NewStyle().Foreground(colBadge).Bold(true)
	styleHelpKey  = lipgloss.NewStyle().Foreground(colAccent)
	styleHelp     = lipgloss.NewStyle().Foreground(colMuted)
	styleRule     = lipgloss.NewStyle().Foreground(colRule)
)

func New(entries []history.Entry, opts Options) model {
	ti := textinput.New()
	ti.Placeholder = "type to search"
	ti.Prompt = ""
	ti.SetValue(opts.Query)
	ti.Focus()
	st := ti.Styles()
	st.Focused.Prompt = lipgloss.NewStyle()
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(colMuted).Italic(true)
	st.Focused.Text = lipgloss.NewStyle().Foreground(colArg)
	ti.SetStyles(st)
	m := model{
		input:  ti,
		all:    entries,
		cwd:    opts.Cwd,
		width:  80,
		height: 24,
		delete: opts.Delete,
	}
	m.refresh()
	return m
}

func Filter(entries []history.Entry, query, cwd string, cwdOnly bool) []history.Entry {
	src := entries
	if cwdOnly && cwd != "" {
		src = make([]history.Entry, 0, len(entries))
		for _, e := range entries {
			if e.Cwd == cwd {
				src = append(src, e)
			}
		}
	}
	ranked := search.Rank(query, src, false)
	out := make([]history.Entry, len(ranked))
	for i, r := range ranked {
		out[i] = r.Entry
	}
	return out
}

func (m *model) refresh() {
	m.refilter()
	m.cursor = len(m.visible) - 1
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *model) refilter() {
	ranked := Filter(m.all, m.input.Value(), m.cwd, m.cwdOnly)
	m.visible = reverseEntries(ranked)
}

func (m *model) clampCursor() {
	if len(m.visible) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *model) deleteSelected() {
	if len(m.visible) == 0 || m.cursor >= len(m.visible) {
		return
	}
	e := m.visible[m.cursor]
	if m.delete != nil {
		if err := m.delete(e); err != nil {
			m.status = err.Error()
			return
		}
	}
	m.status = ""
	kept := make([]history.Entry, 0, len(m.all))
	for _, x := range m.all {
		if x.Command != e.Command {
			kept = append(kept, x)
		}
	}
	m.all = kept
	m.refilter()
	m.clampCursor()
}

func reverseEntries(in []history.Entry) []history.Entry {
	out := make([]history.Entry, len(in))
	for i, e := range in {
		out[len(in)-1-i] = e
	}
	return out
}

func (m model) Init() tea.Cmd {
	return m.input.Focus()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		m.input.SetWidth(max(8, m.width-14))
		return m, nil
	case tea.KeyReleaseMsg:
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			m.print = false
			m.selected = ""
			return m, tea.Quit
		case "enter":
			m.accept(true)
			return m, tea.Quit
		case "ctrl+o":
			m.accept(false)
			return m, tea.Quit
		case "up", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "ctrl+n":
			if m.cursor+1 < len(m.visible) {
				m.cursor++
			}
			return m, nil
		case "tab":
			m.cwdOnly = !m.cwdOnly
			m.refresh()
			return m, nil
		case "ctrl+d":
			m.deleteSelected()
			return m, nil
		}
	}
	prev := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != prev {
		m.refresh()
	}
	return m, cmd
}

func (m *model) accept(run bool) {
	if len(m.visible) > 0 && m.cursor < len(m.visible) {
		m.selected = m.visible[m.cursor].Command
		m.print = true
		m.run = run
	}
	m.quitting = true
}

func (m model) View() tea.View {
	if m.quitting || m.print {
		return tea.NewView("")
	}
	w := max(1, m.width)
	h := max(1, m.height)
	// Leave one column so a full-width line plus '\n' does not wrap
	// (terminals advance to the next row after the last column).
	inner := max(1, w-1)

	header := clampLine(m.renderHeader(inner), inner)
	help := clampLine(m.renderHelp(), inner)
	input := clampLine(m.renderInput(), inner)
	rule := styleRule.Render(strings.Repeat("─", inner))

	chrome := lipgloss.Height(header) + 1 + lipgloss.Height(help) + lipgloss.Height(input)
	listH := h - chrome
	if listH < 0 {
		listH = 0
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	b.WriteString(rule)
	b.WriteByte('\n')
	if listH > 0 {
		b.WriteString(m.renderList(inner, listH))
	}
	b.WriteString(help)
	b.WriteByte('\n')
	b.WriteString(input)

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func clampLine(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	if lipgloss.Width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

func (m model) renderHeader(w int) string {
	scope := "all directories"
	if m.cwdOnly {
		scope = "this directory"
		if m.cwd != "" {
			scope += "  " + m.cwd
		}
	}
	left := styleTitle.Render("syncsh") + "  " + styleMuted.Render(scope)
	right := styleMuted.Render(fmt.Sprintf("%s unique", formatCount(len(m.visible))))
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m model) renderHelp() string {
	key := func(k, label string) string {
		return styleHelpKey.Render(k) + styleHelp.Render(" "+label)
	}
	if m.status != "" {
		return styleFailed.Render(m.status)
	}
	return key("enter", "run") + styleHelp.Render("  ·  ") +
		key("ctrl+o", "edit") + styleHelp.Render("  ·  ") +
		key("tab", "cwd") + styleHelp.Render("  ·  ") +
		key("ctrl+d", "delete") + styleHelp.Render("  ·  ") +
		key("↑↓", "move") + styleHelp.Render("  ·  ") +
		key("esc", "cancel")
}

func (m model) renderInput() string {
	badge := "[ ALL ]"
	if m.cwdOnly {
		badge = "[ DIR ]"
	}
	return styleBadge.Render(badge) + " " + m.input.View()
}

func (m model) renderList(w, listH int) string {
	if len(m.visible) == 0 {
		pad := max(0, listH-1)
		return strings.Repeat("\n", pad) + styleMuted.Render("  no matches") + "\n"
	}
	start, end := m.listWindow(listH)
	now := time.Now()
	var b strings.Builder
	for i := 0; i < listH-(end-start); i++ {
		b.WriteByte('\n')
	}
	for i := start; i < end; i++ {
		b.WriteString(clampLine(m.renderRow(m.visible[i], i == m.cursor, w, now), w))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) listWindow(listH int) (start, end int) {
	n := len(m.visible)
	if n == 0 {
		return 0, 0
	}
	if listH >= n {
		return 0, n
	}
	end = m.cursor + 1
	start = end - listH
	if start < 0 {
		start = 0
		end = listH
	}
	if end > n {
		end = n
		start = n - listH
	}
	return start, end
}

func (m model) renderRow(e history.Entry, selected bool, w int, now time.Time) string {
	durStyle := styleDuration
	if failed(e) {
		durStyle = styleFailed
	}
	marker := " "
	if selected {
		marker = styleAccent.Render("❯")
	}
	dur := alignRight(durStyle.Render(formatDuration(e.DurationMs)), durCol)
	rel := alignRight(styleTime.Render(formatRelative(e.StartTS, now)), relCol)
	prefix := alignRight(marker, markCol) + " " + dur + "  " + rel + "  "
	remain := w - lipgloss.Width(prefix)
	if remain < 8 {
		remain = 8
	}
	cmdText := strings.ReplaceAll(strings.ReplaceAll(e.Command, "\r", ""), "\n", " ")
	cmd := ansi.Truncate(HighlightCommand(cmdText), remain, "…")
	if selected {
		cmd = lipgloss.NewStyle().Bold(true).Render(cmd)
	}
	return prefix + cmd
}

const (
	markCol = 1
	durCol  = 6
	relCol  = 8
)

func alignRight(s string, n int) string {
	w := lipgloss.Width(s)
	if w > n {
		return ansi.Truncate(s, n, "")
	}
	if w < n {
		return strings.Repeat(" ", n-w) + s
	}
	return s
}

func formatCount(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 1000 {
		return s
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (m model) SelectedCommand() (string, bool) {
	if !m.print || m.selected == "" {
		return "", false
	}
	return m.selected, true
}

func (m model) RunSelected() bool {
	return m.print && m.run
}

// AcceptPrefix is printed before the selected command in widget mode so the
// shell integration can execute it (Atuin's __atuin_accept__: protocol).
const AcceptPrefix = "__syncsh_accept__:"

func WriteSelection(out io.Writer, cmd string, run bool) {
	if run {
		fmt.Fprintln(out, AcceptPrefix+cmd)
		return
	}
	fmt.Fprintln(out, cmd)
}

func Run(entries []history.Entry, query string, out io.Writer) error {
	return RunOpts(entries, Options{Query: query}, out)
}

func RunOpts(entries []history.Entry, opts Options, out io.Writer) error {
	m := New(entries, opts)
	var progOpts []tea.ProgramOption
	if opts.Widget {
		in, outTTY, closeFn, err := OpenTTY()
		if err == nil {
			defer closeFn()
			progOpts = append(progOpts, tea.WithInput(in), tea.WithOutput(outTTY))
		}
	}
	p := tea.NewProgram(m, progOpts...)
	final, err := p.Run()
	if err != nil {
		return err
	}
	if got, ok := final.(model); ok {
		if cmd, ok := got.SelectedCommand(); ok {
			WriteSelection(out, cmd, opts.Widget && got.RunSelected())
		}
	}
	return nil
}

func OpenTTY() (in *os.File, out *os.File, closeFn func(), err error) {
	return openTTY()
}
