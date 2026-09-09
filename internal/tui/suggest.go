package tui

import (
	"io"
	"strings"
	"time"

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
}

type suggestItem struct {
	cmd   string
	descr string
}

type suggestModel struct {
	prefix           string
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
	all := make([]suggestItem, 0, len(opts.Items))
	for i, cmd := range opts.Items {
		if cmd == "" {
			continue
		}
		d := ""
		if i < len(opts.Descrs) {
			d = strings.TrimSpace(opts.Descrs[i])
		}
		all = append(all, suggestItem{cmd: cmd, descr: d})
	}
	m := suggestModel{
		prefix:   opts.Prefix,
		allItems: all,
		width:    80,
		height:   10,
		typedIco: typed,
		histIco:  item,
		loading:  opts.ItemsCh != nil,
		itemsCh:  opts.ItemsCh,
	}
	if m.loading {
		m.cursor = 0
	} else {
		m.applyFilter()
	}
	return m
}

func suggestSpinTick() tea.Cmd {
	return tea.Tick(suggestSpinInterval, func(time.Time) tea.Msg {
		return suggestSpinMsg{}
	})
}

func waitSuggestItems(ch <-chan SuggestItems) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return suggestItemsMsg{}
		}
		got, ok := <-ch
		if !ok {
			return suggestItemsMsg{}
		}
		if got.Abort {
			return suggestItemsMsg{abort: true}
		}
		items := make([]suggestItem, 0, len(got.Items))
		for i, cmd := range got.Items {
			if cmd == "" {
				continue
			}
			d := ""
			if i < len(got.Descrs) {
				d = strings.TrimSpace(got.Descrs[i])
			}
			items = append(items, suggestItem{cmd: cmd, descr: d})
		}
		return suggestItemsMsg{items: items}
	}
}

func (m *suggestModel) applyFilter() {
	prefix := m.prefix
	items := make([]suggestItem, 0, len(m.allItems))
	for _, it := range m.allItems {
		if strings.HasPrefix(it.cmd, prefix) && it.cmd != prefix {
			items = append(items, it)
		}
	}
	m.items = items
	if len(m.items) > 0 {
		m.cursor = 1
	} else {
		m.cursor = 0
	}
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

func (m suggestModel) Init() tea.Cmd {
	if !m.loading {
		return nil
	}
	cmds := []tea.Cmd{suggestSpinTick()}
	if m.itemsCh != nil {
		cmds = append(cmds, waitSuggestItems(m.itemsCh))
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
		return m.prefix
	}
	if i-1 < len(m.items) {
		return m.items[i-1].cmd
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
		if msg.abort {
			m.loading = false
			m.quitting = true
			m.print = false
			m.selected = ""
			return m, tea.Quit
		}
		m.loading = false
		m.allItems = msg.items
		m.applyFilter()
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
			m.run = false
			m.cont = false
			m.quitting = true
			return m, tea.Quit
		case "ctrl+space", "ctrl+@", "ctrl+at":
			if m.loading {
				return m, nil
			}
			m.selected = m.commandAt(m.cursor)
			m.print = m.selected != ""
			m.run = false
			m.cont = m.print
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
		case "backspace", "ctrl+h":
			if m.prefix == "" {
				return m, nil
			}
			r := []rune(m.prefix)
			m.prefix = string(r[:len(r)-1])
			if !m.loading {
				m.applyFilter()
			}
			return m, nil
		default:
			if msg.Text != "" && msg.Mod&^(tea.ModShift|tea.ModCapsLock|tea.ModNumLock) == 0 {
				m.prefix += msg.Text
				if !m.loading {
					m.applyFilter()
				}
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

	header := styleTitle.Render("syncsh") + "  " + styleMuted.Render("suggestions")
	if m.loading {
		frame := suggestSpinFrames[m.spinFrame%len(suggestSpinFrames)]
		header = styleTitle.Render("syncsh") + "  " + styleMuted.Render("suggestions") + "  " + styleAccent.Render(frame)
	}
	help := styleHelpKey.Render("enter") + styleHelp.Render(" insert  ") +
		styleHelpKey.Render("ctrl+space") + styleHelp.Render(" complete  ") +
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

func (m suggestModel) renderLoadingRows(inner, listH int) string {
	frame := suggestSpinFrames[m.spinFrame%len(suggestSpinFrames)]
	typed := styleAccent.Render(m.typedIco + " " + m.prefix)
	spin := styleAccent.Render(frame) + " " + styleMuted.Render("loading completions")
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
	labelw := 4
	for i := start; i < end; i++ {
		label := m.prefix
		if i > 0 {
			label = m.items[i-1].cmd
		}
		w := lipgloss.Width(label)
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
		label := m.prefix
		descr := ""
		if i > 0 {
			icon = m.histIco
			label = m.items[i-1].cmd
			descr = m.items[i-1].descr
		}
		if lipgloss.Width(label) > labelw {
			label = ansi.Truncate(label, labelw, "...")
		}
		label = label + strings.Repeat(" ", max(0, labelw-lipgloss.Width(label)))
		cmdPart := icon + " " + label
		switch i {
		case m.cursor:
			cmdPart = styleAccent.Render(cmdPart)
		case 0:
			cmdPart = styleMuted.Render(cmdPart)
		}
		line := cmdPart
		if descr != "" && descBudget > 0 {
			if lipgloss.Width(descr) > descBudget {
				descr = ansi.Truncate(descr, descBudget, "...")
			}
			line = cmdPart + "  " + styleMuted.Render(descr)
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
			if got.ContinueSelected() {
				cmd = ContinuePrefix + cmd
			}
			return writeWidgetSelection(out, Options{Widget: opts.Widget, ResultFile: opts.ResultFile}, cmd, got.RunSelected())
		}
	case *suggestModel:
		if cmd, ok := got.SelectedCommand(); ok {
			if got.ContinueSelected() {
				cmd = ContinuePrefix + cmd
			}
			return writeWidgetSelection(out, Options{Widget: opts.Widget, ResultFile: opts.ResultFile}, cmd, got.RunSelected())
		}
	}
	return nil
}
