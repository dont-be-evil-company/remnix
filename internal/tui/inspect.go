package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dont-be-evil-company/remnix/internal/history"
	"github.com/dont-be-evil-company/remnix/internal/search"
)

type inspectView int

const (
	inspectOverview inspectView = iota
	inspectRuns
	inspectAdvanced
)

type InspectOptions struct {
	Stats            history.Stats
	Summaries        []history.CommandSummary
	Load             func(sort history.SummarySort, query string) ([]history.CommandSummary, history.Stats, error)
	AdvancedLoad     func(sort history.SummarySort, f history.AdvancedFilter) ([]history.CommandSummary, history.Stats, error)
	ListRuns         func(command string) ([]history.Entry, error)
	ListMatchingRuns func(command string, f history.AdvancedFilter) ([]history.Entry, error)
	DeleteCommand    func(command string) error
	DeleteEntry      func(history.Entry) error
	DeleteEntries    func([]history.Entry) error
	StartAdvanced    bool
	Theme            Theme
}

type inspectModel struct {
	input            textinput.Model
	stats            history.Stats
	all              []history.CommandSummary
	visible          []history.CommandSummary
	cursor           int
	sort             history.SummarySort
	view             inspectView
	returnView       inspectView
	pane             inspectPane
	fields           []textinput.Model
	fieldIdx         int
	fieldErrs        []string
	selectedCmds     map[string]struct{}
	selectedRuns     map[string]struct{}
	deletedIDs       map[string]struct{}
	confirmN         int
	confirmKind      string
	confirmEntries   []history.Entry
	chartGran        chartGranularity
	runs             []history.Entry
	runVisible       []history.Entry
	runCursor        int
	runCmd           string
	runSummary       history.CommandSummary
	overviewQuery    string
	width            int
	height           int
	quitting         bool
	status           string
	load             func(sort history.SummarySort, query string) ([]history.CommandSummary, history.Stats, error)
	advancedLoad     func(sort history.SummarySort, f history.AdvancedFilter) ([]history.CommandSummary, history.Stats, error)
	listRuns         func(command string) ([]history.Entry, error)
	listMatchingRuns func(command string, f history.AdvancedFilter) ([]history.Entry, error)
	deleteCommand    func(command string) error
	deleteEntry      func(history.Entry) error
	deleteEntries    func([]history.Entry) error
	lastFilter       history.AdvancedFilter
	theme            Theme
}

func NewInspect(opts InspectOptions) inspectModel {
	th := opts.Theme.OrDefault()
	ti := textinput.New()
	ti.Placeholder = "type to search"
	ti.Prompt = ""
	ti.Focus()
	applyThemeInput(&ti, th)
	m := inspectModel{
		input:            ti,
		stats:            opts.Stats,
		all:              opts.Summaries,
		width:            80,
		height:           24,
		load:             opts.Load,
		advancedLoad:     opts.AdvancedLoad,
		listRuns:         opts.ListRuns,
		listMatchingRuns: opts.ListMatchingRuns,
		deleteCommand:    opts.DeleteCommand,
		deleteEntry:      opts.DeleteEntry,
		deleteEntries:    opts.DeleteEntries,
		theme:            th,
		fields:           newAdvancedFields(th),
		fieldErrs:        make([]string, advancedFieldCount),
		selectedCmds:     map[string]struct{}{},
		selectedRuns:     map[string]struct{}{},
		deletedIDs:       map[string]struct{}{},
	}
	m.refilter()
	m.snapOverview()
	if opts.StartAdvanced {
		m.enterAdvanced()
	}
	return m
}

func (m inspectModel) Init() tea.Cmd {
	if m.view == inspectAdvanced {
		m.input.Blur()
		return nil
	}
	return m.input.Focus()
}

func (m inspectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		m.input.SetWidth(max(8, m.width-16))
		m.resizeAdvancedFields()
		return m, nil
	case tea.FocusMsg, tea.BlurMsg:
		return m, nil
	case tea.KeyReleaseMsg:
		return m, nil
	case tea.KeyPressMsg:
		if m.confirmN > 0 {
			return m.handleConfirm(msg)
		}
		if handled, cmd := m.handleInspectKey(msg); handled {
			return m, cmd
		}
		return m.updateFocusedInput(msg)
	}
	return m, nil
}

func (m *inspectModel) handleInspectKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return true, tea.Quit
	case "ctrl+f":
		if m.view == inspectRuns {
			return true, nil
		}
		if m.view == inspectAdvanced {
			m.leaveAdvanced()
		} else {
			m.enterAdvanced()
		}
		return true, nil
	case "esc":
		if m.view == inspectRuns {
			m.backToOverview()
			return true, nil
		}
		if m.view == inspectAdvanced {
			m.leaveAdvanced()
			return true, nil
		}
		m.quitting = true
		return true, tea.Quit
	case "enter":
		if m.view == inspectAdvanced && m.pane == paneSidebar {
			m.setPane(paneResults)
			return true, nil
		}
		if m.view == inspectOverview || m.view == inspectAdvanced {
			m.openRuns()
			return true, nil
		}
		return true, nil
	case "up", "ctrl+p":
		if m.view == inspectAdvanced && m.pane == paneSidebar {
			m.moveField(-1)
			return true, nil
		}
		m.moveCursor(-1)
		return true, nil
	case "down", "ctrl+n":
		if m.view == inspectAdvanced && m.pane == paneSidebar {
			m.moveField(1)
			return true, nil
		}
		m.moveCursor(1)
		return true, nil
	case "k":
		if m.view == inspectAdvanced && m.pane == paneResults {
			m.moveCursor(-1)
			return true, nil
		}
		return false, nil
	case "j":
		if m.view == inspectAdvanced && m.pane == paneResults {
			m.moveCursor(1)
			return true, nil
		}
		return false, nil
	case "ctrl+w":
		if m.view == inspectAdvanced {
			if m.pane == paneSidebar {
				m.setPane(paneResults)
			} else {
				m.setPane(paneSidebar)
			}
			return true, nil
		}
		return false, nil
	case "tab":
		if m.view == inspectAdvanced && m.pane == paneSidebar {
			m.moveField(1)
			return true, nil
		}
		if m.view == inspectOverview || (m.view == inspectAdvanced && m.pane == paneResults) {
			m.cycleSort()
			return true, nil
		}
		return true, nil
	case "shift+tab":
		if m.view == inspectAdvanced && m.pane == paneSidebar {
			m.moveField(-1)
			return true, nil
		}
		return m.view == inspectAdvanced, nil
	case " ", "space":
		if m.view == inspectAdvanced || m.view == inspectRuns {
			if m.view == inspectAdvanced && m.pane == paneSidebar {
				return false, nil
			}
			m.toggleSelected()
			return true, nil
		}
		return false, nil
	case "ctrl+a":
		if m.view == inspectAdvanced || m.view == inspectRuns {
			if m.view == inspectAdvanced && m.pane == paneSidebar {
				return false, nil
			}
			m.toggleSelectAll()
			return true, nil
		}
		return false, nil
	case "ctrl+d":
		m.moveCursor(m.pageDelta())
		return true, nil
	case "ctrl+u":
		m.moveCursor(-m.pageDelta())
		return true, nil
	case "ctrl+x":
		m.requestDelete()
		return true, nil
	case "g":
		if m.view == inspectRuns {
			m.chartGran = m.chartGran.Next()
			return true, nil
		}
		return false, nil
	}
	return false, nil
}

func (m inspectModel) updateFocusedInput(msg tea.Msg) (inspectModel, tea.Cmd) {
	if m.view == inspectAdvanced && m.pane == paneSidebar && len(m.fields) > 0 {
		prev := m.fields[m.fieldIdx].Value()
		var cmd tea.Cmd
		m.fields[m.fieldIdx], cmd = m.fields[m.fieldIdx].Update(msg)
		if m.fields[m.fieldIdx].Value() != prev {
			m.status = ""
			if m.refilterAdvanced() {
				m.snapOverview()
			} else {
				m.clampCursor()
			}
		}
		return m, cmd
	}
	if m.view == inspectAdvanced {
		return m, nil
	}
	prev := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != prev {
		m.onQueryChange()
	}
	return m, cmd
}

func (m inspectModel) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	if m.view == inspectAdvanced {
		return m.viewAdvanced()
	}
	w := max(1, m.width)
	h := max(1, m.height)
	inner := max(1, w-1)

	header := clampLine(m.renderInspectHeader(inner), inner)
	stats := clampLine(m.renderInspectStats(), inner)
	detail := clampLine(m.renderInspectDetail(), inner)
	help := clampLine(m.renderInspectHelp(), inner)
	input := clampLine(m.renderInspectInput(), inner)
	rule := m.theme.Rule.Render(strings.Repeat("─", inner))
	charts := ""
	if m.view == inspectRuns {
		chartBudget := min(12, max(0, h/3))
		charts = renderChartBlock(m.theme, m.runs, m.chartGran, inner, chartBudget)
	}

	chrome := lipgloss.Height(header) + lipgloss.Height(stats) + 1 + lipgloss.Height(detail) + lipgloss.Height(help) + lipgloss.Height(input)
	if charts != "" {
		chrome += lipgloss.Height(charts)
	}
	listH := h - chrome
	if listH < 0 {
		listH = 0
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	b.WriteString(stats)
	b.WriteByte('\n')
	b.WriteString(rule)
	b.WriteByte('\n')
	if listH > 0 {
		b.WriteString(m.renderInspectList(inner, listH))
	}
	if charts != "" {
		b.WriteString(charts)
		if !strings.HasSuffix(charts, "\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString(detail)
	b.WriteByte('\n')
	b.WriteString(help)
	b.WriteByte('\n')
	b.WriteString(input)

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func RunInspect(opts InspectOptions) error {
	p := tea.NewProgram(NewInspect(opts))
	_, err := p.Run()
	return err
}

func (m *inspectModel) moveCursor(delta int) {
	if m.view == inspectRuns {
		n := len(m.runVisible)
		if n == 0 {
			m.runCursor = 0
			return
		}
		m.runCursor = min(n-1, max(0, m.runCursor+delta))
		return
	}
	n := len(m.visible)
	if n == 0 {
		m.cursor = 0
		return
	}
	m.cursor = min(n-1, max(0, m.cursor+delta))
}

func (m inspectModel) pageDelta() int {
	return max(1, m.height/2)
}

func (m *inspectModel) cycleSort() {
	m.sort = m.sort.Next()
	m.status = ""
	if m.view == inspectAdvanced {
		m.refilterAdvanced()
		m.snapOverview()
		return
	}
	if m.load != nil {
		all, st, err := m.load(m.sort, "")
		if err != nil {
			m.status = err.Error()
			return
		}
		m.all = all
		m.stats = st
	}
	m.refilter()
	m.snapOverview()
}

func (m *inspectModel) onQueryChange() {
	m.status = ""
	if m.view == inspectRuns {
		m.refilterRuns()
		m.snapRuns()
		return
	}
	m.refilter()
	m.snapOverview()
}

func (m *inspectModel) openRuns() {
	if len(m.visible) == 0 || m.cursor >= len(m.visible) {
		return
	}
	s := m.visible[m.cursor]
	m.returnView = m.view
	runs, err := m.loadCurrentRuns(s.Command)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.runs = runs
	if m.view != inspectAdvanced {
		m.overviewQuery = m.input.Value()
	}
	m.input.SetValue("")
	m.input.Placeholder = "filter cwd, host, exit"
	_ = m.input.Focus()
	m.view = inspectRuns
	m.runCmd = s.Command
	m.runSummary = history.SummarizeRuns(s.Command, runs)
	if m.runSummary.Runs == 0 {
		m.runSummary = s
	}
	m.status = ""
	m.selectedRuns = map[string]struct{}{}
	m.refilterRuns()
	m.snapRuns()
}

func (m *inspectModel) backToOverview() {
	prev := m.returnView
	m.runs = nil
	m.runVisible = nil
	m.runCmd = ""
	m.selectedRuns = map[string]struct{}{}
	m.status = ""
	if prev == inspectAdvanced {
		m.view = inspectAdvanced
		m.input.SetValue("")
		m.input.Blur()
		m.refilterAdvanced()
		m.clampCursor()
		return
	}
	m.view = inspectOverview
	m.returnView = inspectOverview
	m.input.SetValue(m.overviewQuery)
	m.input.Placeholder = "type to search"
	_ = m.input.Focus()
	m.reloadBase()
}

func (m *inspectModel) reloadBase() {
	if m.load != nil {
		all, st, err := m.load(m.sort, "")
		if err != nil {
			m.status = err.Error()
			return
		}
		m.all = all
		m.stats = st
	}
	m.refilter()
	m.clampCursor()
}

func (m *inspectModel) deleteSelectedCommand() {
	if len(m.visible) == 0 || m.cursor >= len(m.visible) {
		return
	}
	s := m.visible[m.cursor]
	if m.deleteCommand != nil {
		if err := m.deleteCommand(s.Command); err != nil {
			m.status = err.Error()
			return
		}
	}
	m.status = ""
	if m.load != nil {
		m.reloadBase()
		return
	}
	kept := make([]history.CommandSummary, 0, len(m.all))
	for _, x := range m.all {
		if x.Command != s.Command {
			kept = append(kept, x)
		}
	}
	m.all = kept
	m.stats.Commands -= s.Runs
	if m.stats.Commands < 0 {
		m.stats.Commands = 0
	}
	m.stats.Deleted += s.Runs
	m.stats.Success -= s.Success
	if m.stats.Success < 0 {
		m.stats.Success = 0
	}
	m.stats.Failed -= s.Failed
	if m.stats.Failed < 0 {
		m.stats.Failed = 0
	}
	m.refilter()
	m.clampCursor()
}

func (m *inspectModel) refilter() {
	src := m.all
	q := m.input.Value()
	if m.view == inspectOverview && strings.TrimSpace(q) != "" && m.load != nil {
		extra, _, err := m.load(m.sort, q)
		if err == nil {
			src = mergeSummaries(src, extra)
		}
	}
	m.visible = filterSummaries(src, q, m.sort)
	m.clampCursor()
}

func (m *inspectModel) refilterRuns() {
	m.runVisible = filterRuns(m.runs, m.input.Value())
	m.clampRunCursor()
}

func (m *inspectModel) snapOverview() {
	m.cursor = 0
}

func (m *inspectModel) snapRuns() {
	m.runCursor = 0
}

func (m *inspectModel) clampCursor() {
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

func (m *inspectModel) clampRunCursor() {
	if len(m.runVisible) == 0 {
		m.runCursor = 0
		return
	}
	if m.runCursor >= len(m.runVisible) {
		m.runCursor = len(m.runVisible) - 1
	}
	if m.runCursor < 0 {
		m.runCursor = 0
	}
}

func filterSummaries(in []history.CommandSummary, query string, mode history.SummarySort) []history.CommandSummary {
	src := in
	if mode == history.SortFailed {
		src = make([]history.CommandSummary, 0, len(in))
		for _, s := range in {
			if s.Failed > 0 {
				src = append(src, s)
			}
		}
	}
	if strings.TrimSpace(query) != "" {
		entries := make([]history.Entry, len(src))
		byCmd := make(map[string]history.CommandSummary, len(src))
		for i, s := range src {
			entries[i] = history.Entry{Command: s.Command, StartTS: s.LastTS}
			byCmd[s.Command] = s
		}
		ranked := search.RankWith(query, entries, false, search.Context{})
		matched := make([]history.CommandSummary, 0, len(ranked))
		for _, r := range ranked {
			if s, ok := byCmd[r.Entry.Command]; ok {
				matched = append(matched, s)
			}
		}
		src = matched
	}
	out := append([]history.CommandSummary(nil), src...)
	sortSummaries(out, mode)
	return out
}

func sortSummaries(s []history.CommandSummary, mode history.SummarySort) {
	sort.SliceStable(s, func(i, j int) bool {
		if mode == history.SortTop && s[i].Runs != s[j].Runs {
			return s[i].Runs > s[j].Runs
		}
		if !s[i].LastTS.Equal(s[j].LastTS) {
			return s[i].LastTS.After(s[j].LastTS)
		}
		return s[i].Command < s[j].Command
	})
}

func mergeSummaries(a, b []history.CommandSummary) []history.CommandSummary {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]history.CommandSummary, 0, len(a)+len(b))
	for _, src := range [][]history.CommandSummary{a, b} {
		for _, s := range src {
			if _, ok := seen[s.Command]; ok {
				continue
			}
			seen[s.Command] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}

func filterRuns(in []history.Entry, query string) []history.Entry {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return in
	}
	out := make([]history.Entry, 0, len(in))
	for _, e := range in {
		if strings.Contains(strings.ToLower(e.Cwd), q) ||
			strings.Contains(strings.ToLower(e.Hostname), q) ||
			strings.Contains(strings.ToLower(e.SessionID), q) ||
			strings.Contains(strings.ToLower(e.DeviceID), q) ||
			strings.Contains(strings.ToLower(e.Shell), q) {
			out = append(out, e)
			continue
		}
		if e.ExitStatus != nil && strings.Contains(strconv.Itoa(*e.ExitStatus), q) {
			out = append(out, e)
		}
	}
	return out
}

func (m inspectModel) renderInspectHeader(w int) string {
	left := m.theme.Title.Render("remnix inspect")
	if m.view == inspectAdvanced {
		left += "  " + m.theme.Muted.Render("advanced")
	}
	if m.view == inspectRuns {
		cmd := strings.ReplaceAll(strings.ReplaceAll(m.runCmd, "\r", ""), "\n", " ")
		remain := w - lipgloss.Width(left) - 2
		if remain < 8 {
			remain = 8
		}
		return left + "  " + ansi.Truncate(HighlightCommandTheme(m.theme, cmd), remain, "...")
	}
	right := m.theme.Muted.Render(fmt.Sprintf("%s unique", formatCount(len(m.visible))))
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m inspectModel) renderInspectStats() string {
	if m.view == inspectRuns {
		s := m.runSummary
		return m.theme.Muted.Render(fmt.Sprintf("%s runs  ·  %s ok / %s fail",
			formatCount(int(s.Runs)), formatCount(int(s.Success)), formatCount(int(s.Failed))))
	}
	st := m.stats
	return m.theme.Muted.Render(fmt.Sprintf(
		"commands: %s  deleted: %s  success: %s  failed: %s  devices: %s  sessions: %s",
		formatCount(int(st.Commands)),
		formatCount(int(st.Deleted)),
		formatCount(int(st.Success)),
		formatCount(int(st.Failed)),
		formatCount(int(st.Devices)),
		formatCount(int(st.Sessions)),
	))
}

func (m inspectModel) renderInspectHelp() string {
	key := func(k, label string) string {
		return m.theme.HelpKey.Render(k) + m.theme.Help.Render(" "+label)
	}
	if m.status != "" {
		return m.theme.Failed.Render(m.status)
	}
	if m.confirmN > 0 {
		return m.renderConfirmHelp()
	}
	if m.view == inspectRuns {
		return key("esc", "back") + m.theme.Help.Render("  ·  ") +
			key("space", "select") + m.theme.Help.Render("  ·  ") +
			key("g", "chart") + m.theme.Help.Render("  ·  ") +
			key("ctrl+x", "delete") + m.theme.Help.Render("  ·  ") +
			key(m.theme.Move(), "move") + m.theme.Help.Render("  ·  ") +
			key("ctrl+c", "quit")
	}
	if m.view == inspectAdvanced {
		return key("ctrl+w", "criteria") + m.theme.Help.Render("  ·  ") +
			key("space", "select") + m.theme.Help.Render("  ·  ") +
			key("ctrl+a", "all") + m.theme.Help.Render("  ·  ") +
			key("ctrl+x", "delete") + m.theme.Help.Render("  ·  ") +
			key("enter", "open") + m.theme.Help.Render("  ·  ") +
			key("ctrl+f", "simple") + m.theme.Help.Render("  ·  ") +
			key("esc", "back")
	}
	return key("enter", "open") + m.theme.Help.Render("  ·  ") +
		key("tab", "sort") + m.theme.Help.Render("  ·  ") +
		key("ctrl+f", "advanced") + m.theme.Help.Render("  ·  ") +
		key("ctrl+x", "delete") + m.theme.Help.Render("  ·  ") +
		key(m.theme.Move(), "move") + m.theme.Help.Render("  ·  ") +
		key("esc", "quit")
}

func (m inspectModel) renderInspectInput() string {
	badge := "[ RECENT ]"
	switch {
	case m.view == inspectAdvanced:
		badge = "[ ADVANCED ]"
	case m.view == inspectRuns:
		badge = "[ RUNS ]"
	case m.sort == history.SortTop:
		badge = "[ TOP ]"
	case m.sort == history.SortFailed:
		badge = "[ FAIL ]"
	}
	if m.view == inspectAdvanced {
		hint := m.theme.Muted.Render("ctrl+w criteria · space select")
		if m.pane == paneSidebar {
			hint = m.theme.Muted.Render("tab fields · ctrl+w results")
		}
		return m.theme.Badge.Render(badge) + " " + hint
	}
	return m.theme.Badge.Render(badge) + " " + m.input.View()
}

func (m inspectModel) renderInspectDetail() string {
	now := time.Now()
	if m.view == inspectRuns {
		if len(m.runVisible) == 0 || m.runCursor >= len(m.runVisible) {
			return m.theme.Muted.Render("no runs")
		}
		e := m.runVisible[m.runCursor]
		parts := []string{"id " + e.ID}
		if e.SessionID != "" {
			parts = append(parts, "session "+e.SessionID)
		}
		if e.DeviceID != "" {
			parts = append(parts, "device "+e.DeviceID)
		}
		if e.Shell != "" {
			parts = append(parts, e.Shell)
		}
		return m.theme.Muted.Render(strings.Join(parts, "  ·  "))
	}
	if len(m.visible) == 0 || m.cursor >= len(m.visible) {
		return m.theme.Muted.Render("no matches")
	}
	s := m.visible[m.cursor]
	parts := []string{
		fmt.Sprintf("%s runs", formatCount(int(s.Runs))),
		fmt.Sprintf("%s ok / %s fail", formatCount(int(s.Success)), formatCount(int(s.Failed))),
	}
	if !s.FirstTS.IsZero() {
		parts = append(parts, "first "+formatRelative(s.FirstTS, now))
	}
	if !s.LastTS.IsZero() {
		parts = append(parts, "last "+formatRelative(s.LastTS, now))
	}
	if s.LastCwd != "" {
		parts = append(parts, "cwd "+s.LastCwd)
	}
	if s.LastHost != "" {
		parts = append(parts, "host "+s.LastHost)
	}
	return m.theme.Muted.Render(strings.Join(parts, "  ·  "))
}

func (m inspectModel) renderInspectList(w, listH int) string {
	if m.view == inspectRuns {
		return m.renderRunList(w, listH)
	}
	if len(m.visible) == 0 {
		return m.theme.Muted.Render("  no matches") + strings.Repeat("\n", max(0, listH-1))
	}
	start, end := listWindow(len(m.visible), m.cursor, listH)
	now := time.Now()
	var b strings.Builder
	for i := start; i < end; i++ {
		s := m.visible[i]
		b.WriteString(clampLine(m.renderSummaryRow(s, i == m.cursor, m.view == inspectAdvanced, m.cmdSelected(s.Command), w, now), w))
		b.WriteByte('\n')
	}
	for i := 0; i < listH-(end-start); i++ {
		b.WriteByte('\n')
	}
	return b.String()
}

func (m inspectModel) renderRunList(w, listH int) string {
	if len(m.runVisible) == 0 {
		return m.theme.Muted.Render("  no runs") + strings.Repeat("\n", max(0, listH-1))
	}
	start, end := listWindow(len(m.runVisible), m.runCursor, listH)
	now := time.Now()
	var b strings.Builder
	for i := start; i < end; i++ {
		e := m.runVisible[i]
		b.WriteString(clampLine(m.renderRunRow(e, i == m.runCursor, m.runSelected(e.ID), w, now), w))
		b.WriteByte('\n')
	}
	for i := 0; i < listH-(end-start); i++ {
		b.WriteByte('\n')
	}
	return b.String()
}

func listWindow(n, cursor, listH int) (start, end int) {
	if n == 0 || listH <= 0 {
		return 0, 0
	}
	if listH >= n {
		return 0, n
	}
	start = cursor - listH + 1
	if start < 0 {
		start = 0
	}
	if start > n-listH {
		start = n - listH
	}
	return start, start + listH
}

func (m inspectModel) renderSummaryRow(s history.CommandSummary, selected, showCheck, checked bool, w int, now time.Time) string {
	durStyle := m.theme.Duration
	if s.LastExit != nil && *s.LastExit != 0 {
		durStyle = m.theme.Failed
	}
	marker := " "
	if selected {
		marker = m.theme.Accent.Render(m.theme.Cursor())
	}
	check := ""
	if showCheck {
		var box string
		if checked {
			box = m.theme.Accent.Render("[x]")
		} else {
			box = m.theme.Muted.Render("[ ]")
		}
		check = box + " "
	}
	count := alignRight(m.theme.Muted.Render(formatCount(int(s.Runs))+"×"), inspectCountCol)
	rate := alignRight(m.theme.Muted.Render(successRate(s)), inspectRateCol)
	dur := alignRight(durStyle.Render(formatDuration(s.LastDurationMs)), durCol)
	rel := alignRight(m.theme.Time.Render(formatRelative(s.LastTS, now)), relCol)
	prefix := alignRight(marker, markCol) + " " + check + count + "  " + rate + "  " + dur + "  " + rel + "  "
	remain := w - lipgloss.Width(prefix)
	if remain < 8 {
		remain = 8
	}
	cmdText := strings.ReplaceAll(strings.ReplaceAll(s.Command, "\r", ""), "\n", " ")
	cmd := ansi.Truncate(HighlightCommandTheme(m.theme, cmdText), remain, "...")
	if selected {
		cmd = lipgloss.NewStyle().Bold(true).Render(cmd)
	}
	return prefix + cmd
}

func (m inspectModel) renderRunRow(e history.Entry, selected, checked bool, w int, now time.Time) string {
	durStyle := m.theme.Duration
	if failed(e) {
		durStyle = m.theme.Failed
	}
	marker := " "
	if selected {
		marker = m.theme.Accent.Render(m.theme.Cursor())
	}
	box := m.theme.Muted.Render("[ ]")
	if checked {
		box = m.theme.Accent.Render("[x]")
	}
	dur := alignRight(durStyle.Render(formatDuration(e.DurationMs)), durCol)
	rel := alignRight(m.theme.Time.Render(formatRelative(e.StartTS, now)), relCol)
	exit := alignRight(m.exitLabel(e), inspectExitCol)
	prefix := alignRight(marker, markCol) + " " + box + " " + dur + "  " + rel + "  " + exit + "  "
	remain := w - lipgloss.Width(prefix)
	if remain < 8 {
		remain = 8
	}
	meta := e.Cwd
	if e.Hostname != "" {
		if meta != "" {
			meta += "  "
		}
		meta += e.Hostname
	}
	meta = strings.ReplaceAll(strings.ReplaceAll(meta, "\r", ""), "\n", " ")
	body := ansi.Truncate(m.theme.Muted.Render(meta), remain, "...")
	if selected {
		body = lipgloss.NewStyle().Bold(true).Render(body)
	}
	return prefix + body
}

func successRate(s history.CommandSummary) string {
	n := s.Success + s.Failed
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%d%%", s.Success*100/n)
}

func (m inspectModel) exitLabel(e history.Entry) string {
	if e.ExitStatus == nil {
		return m.theme.Muted.Render("...")
	}
	s := fmt.Sprintf("e%d", *e.ExitStatus)
	if *e.ExitStatus != 0 {
		return m.theme.Failed.Render(s)
	}
	return m.theme.Duration.Render(s)
}

const (
	inspectCountCol = 7
	inspectRateCol  = 4
	inspectExitCol  = 4
)
