package tui

import (
	"io"
	"strings"

	tea "charm.land/bubbletea/v2"
)

type SuggestMenuOptions struct {
	Prefix        string
	Items         []string
	TypedIcon     string
	HistoryIcon   string
	OverlayHeight int
	Widget        bool
	ResultFile    string
}

type suggestModel struct {
	prefix           string
	items            []string
	cursor           int
	width            int
	height           int
	overlay          bool
	overlayY         int
	overlayH         int
	overlayCursorRow int
	overlayOut       io.Writer
	selected         string
	print            bool
	run              bool
	quitting         bool
	typedIco         string
	histIco          string
}

func newSuggestModel(opts SuggestMenuOptions) suggestModel {
	typed := strings.TrimSpace(opts.TypedIcon)
	if typed == "" {
		typed = "›"
	}
	hist := strings.TrimSpace(opts.HistoryIcon)
	if hist == "" {
		hist = "*"
	}
	m := suggestModel{
		prefix:   opts.Prefix,
		items:    opts.Items,
		width:    80,
		height:   10,
		typedIco: typed,
		histIco:  hist,
	}
	if len(m.items) > 0 {
		m.cursor = 1
	}
	return m
}

func (m *suggestModel) EnableOverlay(st OverlayState) {
	m.overlay = true
	m.overlayOut = st.Out
	m.overlayCursorRow = st.CursorRow
	if st.TermCols > 0 {
		m.width = st.TermCols
	}
	if st.TermRows > 0 {
		m.height = st.TermRows
	}
	m.overlayH = st.RectH
	if m.overlayH < 1 || m.overlayH > m.height {
		m.overlayH = min(m.height, max(SearchChromeRows+1, 10))
	}
	m.overlayY = st.RectY
	if m.overlayY < 0 || m.overlayY+m.overlayH > m.height {
		m.overlayY = overlayOriginY(m.overlayCursorRow, m.height, m.width, m.overlayH)
	}
}

func (m suggestModel) Init() tea.Cmd { return nil }

func (m suggestModel) rowCount() int {
	return 1 + len(m.items)
}

func (m suggestModel) commandAt(i int) string {
	if i <= 0 {
		return m.prefix
	}
	if i-1 < len(m.items) {
		return m.items[i-1]
	}
	return m.prefix
}

func (m suggestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		if !m.overlay {
			m.height = max(1, msg.Height)
		} else {
			m.height = max(1, msg.Height)
			if m.overlayH < 1 || m.overlayH > m.height {
				m.overlayH = min(m.height, max(SearchChromeRows+1, 10))
			}
			m.overlayY = overlayOriginY(m.overlayCursorRow, m.height, m.width, m.overlayH)
		}
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
			m.selected = m.commandAt(m.cursor)
			m.print = m.selected != ""
			m.run = m.cursor > 0
			m.quitting = true
			return m, tea.Quit
		case "ctrl+o":
			m.selected = m.commandAt(m.cursor)
			m.print = m.selected != ""
			m.run = false
			m.quitting = true
			return m, tea.Quit
		case "up", "ctrl+p":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "ctrl+n":
			if m.cursor+1 < m.rowCount() {
				m.cursor++
			}
			return m, nil
		}
	}
	return m, nil
}

func (m suggestModel) SelectedCommand() (string, bool) {
	if !m.print || m.selected == "" {
		return "", false
	}
	return m.selected, true
}

func (m suggestModel) RunSelected() bool {
	return m.print && m.run
}

func (m suggestModel) View() tea.View {
	if m.quitting || m.print {
		return tea.NewView("")
	}
	w := max(1, m.width)
	inner := max(1, w-1)
	h := max(1, m.height)
	if m.overlay {
		h = max(1, m.overlayH)
	}

	header := styleTitle.Render("syncsh") + "  " + styleMuted.Render("suggestions")
	help := styleHelpKey.Render("enter") + styleHelp.Render(" run  ") +
		styleHelpKey.Render("ctrl+o") + styleHelp.Render(" insert  ") +
		styleHelpKey.Render("esc") + styleHelp.Render(" cancel")

	chrome := 3
	listH := h - chrome
	if listH < 1 {
		listH = 1
	}

	var b strings.Builder
	b.WriteString(clampLine(header, inner))
	b.WriteByte('\n')
	b.WriteString(m.renderSuggestRows(inner, listH))
	b.WriteString(clampLine(help, inner))

	v := tea.NewView(b.String())
	v.AltScreen = !m.overlay
	if m.overlay {
		drawFixed(m.overlayOut, m.width, m.overlayY, m.overlayH, v.Content)
	}
	return v
}

func (m suggestModel) renderSuggestRows(inner, listH int) string {
	n := m.rowCount()
	start := 0
	if n > listH {
		start = m.cursor - listH/2
		if start < 0 {
			start = 0
		}
		if start+listH > n {
			start = n - listH
		}
	}
	end := start + listH
	if end > n {
		end = n
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		icon := m.typedIco
		text := m.prefix
		if i > 0 {
			icon = m.histIco
			text = m.items[i-1]
		}
		line := icon + " " + text
		if i == m.cursor {
			line = styleAccent.Render(line)
		} else if i == 0 {
			line = styleMuted.Render(line)
		}
		b.WriteString(clampLine(line, inner))
		b.WriteByte('\n')
	}
	for i := end - start; i < listH; i++ {
		b.WriteByte('\n')
	}
	return b.String()
}

func RunSuggestMenu(opts SuggestMenuOptions, out io.Writer) error {
	m := newSuggestModel(opts)
	var (
		p       *tea.Program
		cleanup func()
		err     error
	)
	if opts.Widget {
		p, cleanup, err = widgetProgram(&m, func(termRows int) int {
			h := opts.OverlayHeight
			if h <= 0 {
				h = max(SearchChromeRows+1, min(len(opts.Items)+3, 12))
			}
			if h > termRows {
				return termRows
			}
			return h
		})
		if err != nil {
			return err
		}
		defer cleanup()
	} else {
		p = tea.NewProgram(m)
	}
	final, err := p.Run()
	if err != nil {
		return err
	}
	switch got := final.(type) {
	case suggestModel:
		if cmd, ok := got.SelectedCommand(); ok {
			return writeWidgetSelection(out, Options{Widget: opts.Widget, ResultFile: opts.ResultFile}, cmd, got.RunSelected())
		}
	case *suggestModel:
		if cmd, ok := got.SelectedCommand(); ok {
			return writeWidgetSelection(out, Options{Widget: opts.Widget, ResultFile: opts.ResultFile}, cmd, got.RunSelected())
		}
	}
	return nil
}
