package tui

import (
	"io"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

var suggestSpinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

const suggestSpinInterval = 80 * time.Millisecond

type suggestSpinMsg struct{}

// SuggestItems is a late payload for the overlay: compsys runs after the
// spinner is already on screen, then these replace the loading state.
type SuggestItems struct {
	Items  []string
	Descrs []string
	Abort  bool
}

type suggestItemsMsg struct {
	items []suggestItem
	abort bool
	eof   bool
}

type SuggestMenuOptions struct {
	Prefix        string
	Items         []string
	Descrs        []string
	TypedIcon     string
	HistoryIcon   string
	ItemIcon      string
	OverlayHeight int
	Widget        bool
	ResultFile    string
	// ItemsCh, when set, opens the menu in a loading state until a payload
	// arrives (or Abort). Used so compsys can run while the spinner paints.
	ItemsCh <-chan SuggestItems
	// Cache, when set, is shared across overlay continue RPCs so backspace can
	// restore parent completions and Ctrl+Space can drill cached children
	// without re-running the completer.
	Cache *SuggestCache
	Theme Theme
}

type filterCursor int

const (
	filterCursorFirst filterCursor = iota
	filterCursorTyped
)

type suggestItem struct {
	cmd   string
	descr string
}

type suggestModel struct {
	input            textinput.Model
	items            []suggestItem
	allItems         []suggestItem
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
	cont             bool
	loading          bool
	spinFrame        int
	itemsCh          <-chan SuggestItems
	cache            *SuggestCache
	loadedPrefix     string
	theme            Theme
	detail           bool
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
	item := strings.TrimSpace(opts.ItemIcon)
	if item == "" {
		item = hist
	}
	all := suggestItemsFrom(opts.Items, opts.Descrs)
	cache := opts.Cache
	if cache == nil {
		cache = NewSuggestCache()
	}
	th := opts.Theme.OrDefault()
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = ""
	ti.SetValue(opts.Prefix)
	ti.CursorEnd()
	ti.Focus()
	applyThemeInput(&ti, th)
	m := suggestModel{
		input:    ti,
		allItems: all,
		width:    80,
		height:   10,
		typedIco: typed,
		histIco:  item,
		loading:  opts.ItemsCh != nil,
		itemsCh:  opts.ItemsCh,
		cache:    cache,
		theme:    th,
	}
	m.syncInputWidth()
	if m.loading {
		m.cursor = 0
	} else {
		m.cache.store(opts.Prefix, all)
		m.applyFilter(filterCursorFirst)
	}
	return m
}

func (m *suggestModel) syncInputWidth() {
	inner := max(1, m.width-1)
	ico := lipgloss.Width(m.typedIco + " ")
	// Leave one cell for the virtual caret so clampLine does not ellipsize the row.
	m.input.SetWidth(max(8, inner-ico-1))
}

func (m suggestModel) prefix() string {
	return m.input.Value()
}

func (m *suggestModel) setPrefix(s string) {
	m.input.SetValue(s)
	m.input.CursorEnd()
}

func suggestSpinTick() tea.Cmd {
	return tea.Tick(suggestSpinInterval, func(time.Time) tea.Msg {
		return suggestSpinMsg{}
	})
}

func waitSuggestItems(ch <-chan SuggestItems) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return suggestItemsMsg{eof: true}
		}
		got, ok := <-ch
		if !ok {
			return suggestItemsMsg{eof: true}
		}
		if got.Abort {
			return suggestItemsMsg{abort: true}
		}
		return suggestItemsMsg{items: suggestItemsFrom(got.Items, got.Descrs)}
	}
}

func (m *suggestModel) applyFilter(cur filterCursor) {
	source := m.allItems
	p := m.prefix()
	if _, items, ok := m.cache.best(p); ok {
		source = items
	}
	filtered := make([]suggestItem, 0, len(source))
	for _, it := range source {
		if strings.HasPrefix(it.cmd, p) && it.cmd != p {
			filtered = append(filtered, it)
		}
	}
	m.items = filtered
	if cur == filterCursorTyped {
		m.cursor = 0
		return
	}
	if len(m.items) > 0 {
		m.cursor = 1
	} else {
		m.cursor = 0
	}
}

func drillPrefix(cmd string) string {
	if cmd == "" || strings.HasSuffix(cmd, " ") {
		return cmd
	}
	return cmd + " "
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
	m.syncInputWidth()
}

func (m suggestModel) Init() tea.Cmd {
	cmds := []tea.Cmd{m.input.Focus()}
	if m.loading {
		cmds = append(cmds, suggestSpinTick())
		if m.itemsCh != nil {
			cmds = append(cmds, waitSuggestItems(m.itemsCh))
		}
	}
	return tea.Batch(cmds...)
}

func (m suggestModel) rowCount() int {
	if m.loading {
		return 1
	}
	return 1 + len(m.items)
}

func (m suggestModel) commandAt(i int) string {
	if i <= 0 {
		return m.prefix()
	}
	if i-1 < len(m.items) {
		return m.items[i-1].cmd
	}
	return m.prefix()
}

func (m suggestModel) selectedItem() (suggestItem, bool) {
	if m.cursor <= 0 || m.cursor-1 >= len(m.items) {
		return suggestItem{}, false
	}
	return m.items[m.cursor-1], true
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
		m.syncInputWidth()
		return m, nil
	case tea.FocusMsg, tea.BlurMsg:
		return m, nil
	case tea.KeyReleaseMsg:
		return m, nil
	case suggestSpinMsg:
		if !m.loading {
			return m, nil
		}
		m.spinFrame = (m.spinFrame + 1) % len(suggestSpinFrames)
		return m, suggestSpinTick()
	case suggestItemsMsg:
		if msg.eof {
			return m, nil
		}
		if msg.abort {
			m.loading = false
			m.quitting = true
			m.print = false
			m.selected = ""
			return m, tea.Quit
		}
		first := m.loading
		m.loading = false
		if m.loadedPrefix == "" {
			m.loadedPrefix = m.prefix()
		}
		prev := ""
		if !first {
			prev = m.commandAt(m.cursor)
		}
		m.allItems = msg.items
		m.cache.store(m.loadedPrefix, msg.items)
		if first {
			m.applyFilter(filterCursorFirst)
		} else if m.cursor == 0 {
			m.applyFilter(filterCursorTyped)
		} else {
			m.applyFilter(filterCursorFirst)
			for i, it := range m.items {
				if it.cmd == prev {
					m.cursor = i + 1
					break
				}
			}
		}
		return m, waitSuggestItems(m.itemsCh)
	case tea.KeyPressMsg:
		if m.detail {
			switch msg.String() {
			case "esc", "ctrl+d":
				m.detail = false
				return m, nil
			case "ctrl+c":
				m.detail = false
				m.quitting = true
				m.print = false
				m.selected = ""
				return m, tea.Quit
			default:
				return m, nil
			}
		}
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			m.print = false
			m.selected = ""
			return m, tea.Quit
		case "enter":
			m.selected = m.commandAt(m.cursor)
			m.print = m.selected != ""
			m.run = false
			m.cont = false
			m.quitting = true
			return m, tea.Quit
		case "ctrl+space", "ctrl+@", "ctrl+at":
			if m.loading {
				return m, nil
			}
			sel := m.commandAt(m.cursor)
			if m.cursor <= 0 {
				if m.cache.Has(m.prefix()) {
					return m, nil
				}
			} else if m.cache.Has(sel) {
				m.setPrefix(drillPrefix(sel))
				m.applyFilter(filterCursorFirst)
				return m, nil
			}
			m.selected = sel
			m.print = sel != ""
			m.run = false
			m.cont = m.print
			m.quitting = true
			return m, tea.Quit
		case "ctrl+d":
			if it, ok := m.selectedItem(); ok && strings.TrimSpace(it.descr) != "" {
				m.detail = true
			}
			return m, nil
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
		prev := m.prefix()
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.prefix() != prev && !m.loading {
			if len(m.prefix()) < len(prev) {
				m.applyFilter(filterCursorTyped)
			} else {
				m.applyFilter(filterCursorFirst)
			}
		}
		return m, cmd
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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

func (m suggestModel) ContinueSelected() bool {
	return m.print && m.cont
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

	header := m.theme.Title.Render("remnix") + "  " + m.theme.Muted.Render("suggestions")
	if m.detail {
		header = m.theme.Title.Render("remnix") + "  " + m.theme.Muted.Render("detail")
	} else if m.loading {
		frame := suggestSpinFrames[m.spinFrame%len(suggestSpinFrames)]
		header = m.theme.Title.Render("remnix") + "  " + m.theme.Muted.Render("suggestions") + "  " + m.theme.Accent.Render(frame)
	}
	var help string
	if m.detail {
		help = m.theme.HelpKey.Render("ctrl+d") + m.theme.Help.Render(" close  ") +
			m.theme.HelpKey.Render("esc") + m.theme.Help.Render(" close")
	} else {
		help = m.theme.HelpKey.Render("enter") + m.theme.Help.Render(" insert  ") +
			m.theme.HelpKey.Render("ctrl+space") + m.theme.Help.Render(" complete  ") +
			m.theme.HelpKey.Render("ctrl+d") + m.theme.Help.Render(" detail  ") +
			m.theme.HelpKey.Render("ctrl+w") + m.theme.Help.Render(" word  ") +
			m.theme.HelpKey.Render("esc") + m.theme.Help.Render(" cancel")
	}

	chrome := 3
	listH := h - chrome
	if listH < 1 {
		listH = 1
	}

	var b strings.Builder
	b.WriteString(clampLine(header, inner))
	b.WriteByte('\n')
	if m.detail {
		b.WriteString(m.renderDetailRows(inner, listH))
	} else {
		b.WriteString(m.renderSuggestRows(inner, listH))
	}
	b.WriteString(clampLine(help, inner))

	v := tea.NewView(b.String())
	v.AltScreen = !m.overlay
	if m.overlay {
		drawFixed(m.overlayOut, m.width, m.overlayY, m.overlayH, v.Content)
	}
	return v
}

func (m suggestModel) typedInputView(selected bool) string {
	th := m.theme
	if selected {
		th = th.ForSelect()
	}
	st := m.input.Styles()
	st.Focused.Prompt = lipgloss.NewStyle()
	st.Focused.Placeholder = th.Muted.Italic(true)
	if selected {
		st.Focused.Text = th.Accent
	} else {
		st.Focused.Text = th.Muted
	}
	m.input.SetStyles(st)
	return m.input.View()
}

func (m suggestModel) renderDetailRows(inner, listH int) string {
	it, ok := m.selectedItem()
	if !ok {
		var b strings.Builder
		for i := 0; i < listH; i++ {
			b.WriteByte('\n')
		}
		return b.String()
	}
	type detailLine struct {
		text   string
		accent bool
	}
	var lines []detailLine
	for _, part := range strings.Split(ansi.Wrap(it.cmd, inner, ""), "\n") {
		lines = append(lines, detailLine{text: part, accent: true})
	}
	if d := strings.TrimSpace(it.descr); d != "" {
		if len(lines) > 0 {
			lines = append(lines, detailLine{})
		}
		for _, part := range strings.Split(ansi.Wrap(d, inner, ""), "\n") {
			lines = append(lines, detailLine{text: part})
		}
	}
	if len(lines) > listH {
		lines = lines[:listH]
		if listH > 0 {
			lines[listH-1].text = ansi.Truncate(lines[listH-1].text, max(1, inner), "...")
			lines[listH-1].accent = false
		}
	}
	var b strings.Builder
	for _, line := range lines {
		text := clampLine(line.text, inner)
		if line.accent {
			text = m.theme.Accent.Render(text)
		} else if text != "" {
			text = m.theme.Muted.Render(text)
		}
		b.WriteString(text)
		b.WriteByte('\n')
	}
	for i := len(lines); i < listH; i++ {
		b.WriteByte('\n')
	}
	return b.String()
}

func (m suggestModel) renderLoadingRows(inner, listH int) string {
	frame := suggestSpinFrames[m.spinFrame%len(suggestSpinFrames)]
	typed := m.theme.Accent.Render(m.typedIco+" ") + m.typedInputView(true)
	spin := m.theme.Accent.Render(frame) + " " + m.theme.Muted.Render("loading completions")
	var b strings.Builder
	b.WriteString(clampLine(typed, inner))
	b.WriteByte('\n')
	used := 1
	if listH > 1 {
		b.WriteString(clampLine(spin, inner))
		b.WriteByte('\n')
		used++
	}
	for i := used; i < listH; i++ {
		b.WriteByte('\n')
	}
	return b.String()
}

func (m suggestModel) renderSuggestRows(inner, listH int) string {
	if m.loading {
		return m.renderLoadingRows(inner, listH)
	}
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
	prefix := m.prefix()
	labelw := 4
	for i := start; i < end; i++ {
		label := prefix
		if i > 0 {
			label = m.items[i-1].cmd
		}
		w := lipgloss.Width(label)
		if i == 0 {
			w = lipgloss.Width(prefix) + 1
		}
		if w > labelw {
			labelw = w
		}
	}
	descBudget := inner - (2 + labelw + 2)
	if descBudget < 8 {
		overflow := 8 - descBudget
		labelw -= overflow
		if labelw < 4 {
			labelw = 4
		}
		descBudget = inner - (2 + labelw + 2)
		if descBudget < 0 {
			descBudget = 0
		}
	}
	var b strings.Builder
	for i := start; i < end; i++ {
		icon := m.typedIco
		label := prefix
		descr := ""
		if i > 0 {
			icon = m.histIco
			label = m.items[i-1].cmd
			descr = m.items[i-1].descr
		}
		if i != 0 && lipgloss.Width(label) > labelw {
			label = ansi.Truncate(label, labelw, "...")
		}
		label = label + strings.Repeat(" ", max(0, labelw-lipgloss.Width(label)))
		th := m.theme
		if i == m.cursor {
			th = th.ForSelect()
		}
		var cmdPart string
		if i == 0 {
			cmdPart = m.theme.Muted.Render(icon + " ")
			if i == m.cursor {
				cmdPart = th.Accent.Render(icon + " ")
			}
			cmdPart += m.typedInputView(i == m.cursor)
		} else {
			cmdPart = icon + " " + label
			if i == m.cursor {
				cmdPart = th.Accent.Render(cmdPart)
			}
		}
		line := cmdPart
		if descr != "" && descBudget > 0 {
			if lipgloss.Width(descr) > descBudget {
				descr = ansi.Truncate(descr, descBudget, "...")
			}
			style := m.theme.Muted
			if i == m.cursor {
				style = th.Muted
			}
			line = cmdPart + "  " + style.Render(descr)
		}
		line = clampLine(line, inner)
		if i == m.cursor {
			line = m.theme.PaintSelect(line, inner)
		}
		b.WriteString(line)
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
			if got.ContinueSelected() {
				cmd = ContinuePrefix + ContinueBuffer(cmd)
			}
			return writeWidgetSelection(out, Options{Widget: opts.Widget, ResultFile: opts.ResultFile}, cmd, got.RunSelected())
		}
	case *suggestModel:
		if cmd, ok := got.SelectedCommand(); ok {
			if got.ContinueSelected() {
				cmd = ContinuePrefix + ContinueBuffer(cmd)
			}
			return writeWidgetSelection(out, Options{Widget: opts.Widget, ResultFile: opts.ResultFile}, cmd, got.RunSelected())
		}
	}
	return nil
}
