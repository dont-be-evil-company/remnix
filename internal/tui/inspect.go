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

	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/search"
)

type inspectView int

const (
	inspectOverview inspectView = iota
	inspectRuns
)

type InspectOptions struct {
	Stats         history.Stats
	Summaries     []history.CommandSummary
	Load          func(sort history.SummarySort, query string) ([]history.CommandSummary, history.Stats, error)
	ListRuns      func(command string) ([]history.Entry, error)
	DeleteCommand func(command string) error
	DeleteEntry   func(history.Entry) error
}

type inspectModel struct {
	input         textinput.Model
	stats         history.Stats
	all           []history.CommandSummary
	visible       []history.CommandSummary
	cursor        int
	sort          history.SummarySort
	view          inspectView
	runs          []history.Entry
	runVisible    []history.Entry
	runCursor     int
	runCmd        string
	runSummary    history.CommandSummary
	overviewQuery string
	width         int
	height        int
	quitting      bool
	status        string
	load          func(sort history.SummarySort, query string) ([]history.CommandSummary, history.Stats, error)
	listRuns      func(command string) ([]history.Entry, error)
	deleteCommand func(command string) error
	deleteEntry   func(history.Entry) error
}

func NewInspect(opts InspectOptions) inspectModel {
	ti := textinput.New()
	ti.Placeholder = "type to search"
	ti.Prompt = ""
	ti.Focus()
	st := ti.Styles()
	st.Focused.Prompt = lipgloss.NewStyle()
	st.Focused.Placeholder = lipgloss.NewStyle().Foreground(colMuted).Italic(true)
	st.Focused.Text = lipgloss.NewStyle().Foreground(colArg)
	ti.SetStyles(st)
	m := inspectModel{
		input:         ti,
		stats:         opts.Stats,
		all:           opts.Summaries,
		width:         80,
		height:        24,
		load:          opts.Load,
		listRuns:      opts.ListRuns,
		deleteCommand: opts.DeleteCommand,
		deleteEntry:   opts.DeleteEntry,
	}
	m.refilter()
	m.snapOverview()
	return m
}

func (m inspectModel) Init() tea.Cmd {
	return m.input.Focus()
}

func (m inspectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(1, msg.Width)
		m.height = max(1, msg.Height)
		m.input.SetWidth(max(8, m.width-16))
		return m, nil
	case tea.FocusMsg, tea.BlurMsg:
		return m, nil
	case tea.KeyReleaseMsg:
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			if m.view == inspectRuns {
				m.backToOverview()
				return m, nil
			}
			m.quitting = true
			return m, tea.Quit
		case "enter":
			if m.view == inspectOverview {
				m.openRuns()
			}
			return m, nil
		case "up", "ctrl+p":
			m.moveCursor(-1)
			return m, nil
		case "down", "ctrl+n":
			m.moveCursor(1)
			return m, nil
		case "tab":
			if m.view == inspectOverview {
				m.cycleSort()
			}
			return m, nil
		case "ctrl+d":
			if m.view == inspectRuns {
				m.deleteSelectedRun()
			} else {
				m.deleteSelectedCommand()
			}
			return m, nil
		}
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
	w := max(1, m.width)
	h := max(1, m.height)
	inner := max(1, w-1)

	header := clampLine(m.renderInspectHeader(inner), inner)
	stats := clampLine(m.renderInspectStats(), inner)
	detail := clampLine(m.renderInspectDetail(), inner)
	help := clampLine(m.renderInspectHelp(), inner)
	input := clampLine(m.renderInspectInput(), inner)
	rule := styleRule.Render(strings.Repeat("─", inner))

	chrome := lipgloss.Height(header) + lipgloss.Height(stats) + 1 + lipgloss.Height(detail) + lipgloss.Height(help) + lipgloss.Height(input)
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

func (m *inspectModel) cycleSort() {
	m.sort = m.sort.Next()
	m.status = ""
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
	if m.listRuns != nil {
		runs, err := m.listRuns(s.Command)
		if err != nil {
			m.status = err.Error()
			return
		}
		m.runs = runs
	} else {
		m.runs = nil
	}
	m.overviewQuery = m.input.Value()
	m.input.SetValue("")
	m.input.Placeholder = "filter cwd, host, exit"
	m.view = inspectRuns
	m.runCmd = s.Command
	m.runSummary = s
	m.status = ""
	m.refilterRuns()
	m.snapRuns()
}

func (m *inspectModel) backToOverview() {
	m.view = inspectOverview
	m.runs = nil
	m.runVisible = nil
	m.runCmd = ""
	m.input.SetValue(m.overviewQuery)
	m.input.Placeholder = "type to search"
	m.status = ""
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

func (m *inspectModel) deleteSelectedRun() {
	if len(m.runVisible) == 0 || m.runCursor >= len(m.runVisible) {
		return
	}
	e := m.runVisible[m.runCursor]
	if m.deleteEntry != nil {
		if err := m.deleteEntry(e); err != nil {
			m.status = err.Error()
			return
		}
	}
	m.status = ""
	kept := make([]history.Entry, 0, len(m.runs))
	for _, x := range m.runs {
		if x.ID != e.ID {
			kept = append(kept, x)
		}
	}
	m.runs = kept
	m.runSummary.Runs--
	if e.ExitStatus != nil {
		if *e.ExitStatus == 0 {
			m.runSummary.Success--
		} else {
			m.runSummary.Failed--
		}
	}
	if m.runSummary.Runs < 0 {
		m.runSummary.Runs = 0
	}
	if m.stats.Commands > 0 {
		m.stats.Commands--
	}
	m.stats.Deleted++
	if e.ExitStatus != nil {
		if *e.ExitStatus == 0 && m.stats.Success > 0 {
			m.stats.Success--
		} else if *e.ExitStatus != 0 && m.stats.Failed > 0 {
			m.stats.Failed--
		}
	}
	if len(m.runs) == 0 {
		if m.load == nil {
			keptSum := make([]history.CommandSummary, 0, len(m.all))
			for _, x := range m.all {
				if x.Command != m.runCmd {
					keptSum = append(keptSum, x)
				}
			}
			m.all = keptSum
		}
		m.backToOverview()
		return
	}
	if m.listRuns != nil {
		runs, err := m.listRuns(m.runCmd)
		if err != nil {
			m.status = err.Error()
		} else {
			m.runs = runs
		}
	}
	m.refilterRuns()
	m.clampRunCursor()
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
	m.visible = reverseSummaries(filterSummaries(src, q, m.sort))
	m.clampCursor()
}

func (m *inspectModel) refilterRuns() {
	m.runVisible = reverseEntries(filterRuns(m.runs, m.input.Value()))
	m.clampRunCursor()
}

func (m *inspectModel) snapOverview() {
	m.cursor = lastIndex(len(m.visible))
}

func (m *inspectModel) snapRuns() {
	m.runCursor = lastIndex(len(m.runVisible))
}

func lastIndex(n int) int {
	if n <= 0 {
		return 0
	}
	return n - 1
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

func reverseSummaries(in []history.CommandSummary) []history.CommandSummary {
	out := make([]history.CommandSummary, len(in))
	for i, s := range in {
		out[len(in)-1-i] = s
	}
	return out
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
	left := styleTitle.Render("syncsh inspect")
	if m.view == inspectRuns {
		cmd := strings.ReplaceAll(strings.ReplaceAll(m.runCmd, "\r", ""), "\n", " ")
		remain := w - lipgloss.Width(left) - 2
		if remain < 8 {
			remain = 8
		}
		return left + "  " + ansi.Truncate(HighlightCommand(cmd), remain, "...")
	}
	right := styleMuted.Render(fmt.Sprintf("%s unique", formatCount(len(m.visible))))
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m inspectModel) renderInspectStats() string {
	if m.view == inspectRuns {
		s := m.runSummary
		return styleMuted.Render(fmt.Sprintf("%s runs  ·  %s ok / %s fail",
			formatCount(int(s.Runs)), formatCount(int(s.Success)), formatCount(int(s.Failed))))
	}
	st := m.stats
	return styleMuted.Render(fmt.Sprintf(
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
		return styleHelpKey.Render(k) + styleHelp.Render(" "+label)
	}
	if m.status != "" {
		return styleFailed.Render(m.status)
	}
	if m.view == inspectRuns {
		return key("esc", "back") + styleHelp.Render("  ·  ") +
			key("ctrl+d", "delete run") + styleHelp.Render("  ·  ") +
			key("↑↓", "move") + styleHelp.Render("  ·  ") +
			key("ctrl+c", "quit")
	}
	return key("enter", "open") + styleHelp.Render("  ·  ") +
		key("tab", "sort") + styleHelp.Render("  ·  ") +
		key("ctrl+d", "delete") + styleHelp.Render("  ·  ") +
		key("↑↓", "move") + styleHelp.Render("  ·  ") +
		key("esc", "quit")
}

func (m inspectModel) renderInspectInput() string {
	badge := "[ RECENT ]"
	switch {
	case m.view == inspectRuns:
		badge = "[ RUNS ]"
	case m.sort == history.SortTop:
		badge = "[ TOP ]"
	case m.sort == history.SortFailed:
		badge = "[ FAIL ]"
	}
	return styleBadge.Render(badge) + " " + m.input.View()
}

func (m inspectModel) renderInspectDetail() string {
	now := time.Now()
	if m.view == inspectRuns {
		if len(m.runVisible) == 0 || m.runCursor >= len(m.runVisible) {
			return styleMuted.Render("no runs")
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
		return styleMuted.Render(strings.Join(parts, "  ·  "))
	}
	if len(m.visible) == 0 || m.cursor >= len(m.visible) {
		return styleMuted.Render("no matches")
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
	return styleMuted.Render(strings.Join(parts, "  ·  "))
}

func (m inspectModel) renderInspectList(w, listH int) string {
	if m.view == inspectRuns {
		return m.renderRunList(w, listH)
	}
	if len(m.visible) == 0 {
		pad := max(0, listH-1)
		return strings.Repeat("\n", pad) + styleMuted.Render("  no matches") + "\n"
	}
	start, end := listWindow(len(m.visible), m.cursor, listH)
	now := time.Now()
	var b strings.Builder
	for i := 0; i < listH-(end-start); i++ {
		b.WriteByte('\n')
	}
	for i := start; i < end; i++ {
		b.WriteString(clampLine(m.renderSummaryRow(m.visible[i], i == m.cursor, w, now), w))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m inspectModel) renderRunList(w, listH int) string {
	if len(m.runVisible) == 0 {
		pad := max(0, listH-1)
		return strings.Repeat("\n", pad) + styleMuted.Render("  no runs") + "\n"
	}
	start, end := listWindow(len(m.runVisible), m.runCursor, listH)
	now := time.Now()
	var b strings.Builder
	for i := 0; i < listH-(end-start); i++ {
		b.WriteByte('\n')
	}
	for i := start; i < end; i++ {
		b.WriteString(clampLine(m.renderRunRow(m.runVisible[i], i == m.runCursor, w, now), w))
		b.WriteByte('\n')
	}
	return b.String()
}

func listWindow(n, cursor, listH int) (start, end int) {
	if n == 0 {
		return 0, 0
	}
	if listH >= n {
		return 0, n
	}
	end = cursor + 1
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

func (m inspectModel) renderSummaryRow(s history.CommandSummary, selected bool, w int, now time.Time) string {
	durStyle := styleDuration
	if s.LastExit != nil && *s.LastExit != 0 {
		durStyle = styleFailed
	}
	marker := " "
	if selected {
		marker = styleAccent.Render("❯")
	}
	count := alignRight(styleMuted.Render(formatCount(int(s.Runs))+"×"), inspectCountCol)
	rate := alignRight(styleMuted.Render(successRate(s)), inspectRateCol)
	dur := alignRight(durStyle.Render(formatDuration(s.LastDurationMs)), durCol)
	rel := alignRight(styleTime.Render(formatRelative(s.LastTS, now)), relCol)
	prefix := alignRight(marker, markCol) + " " + count + "  " + rate + "  " + dur + "  " + rel + "  "
	remain := w - lipgloss.Width(prefix)
	if remain < 8 {
		remain = 8
	}
	cmdText := strings.ReplaceAll(strings.ReplaceAll(s.Command, "\r", ""), "\n", " ")
	cmd := ansi.Truncate(HighlightCommand(cmdText), remain, "...")
	if selected {
		cmd = lipgloss.NewStyle().Bold(true).Render(cmd)
	}
	return prefix + cmd
}

func (m inspectModel) renderRunRow(e history.Entry, selected bool, w int, now time.Time) string {
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
	exit := alignRight(exitLabel(e), inspectExitCol)
	prefix := alignRight(marker, markCol) + " " + dur + "  " + rel + "  " + exit + "  "
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
	body := ansi.Truncate(styleMuted.Render(meta), remain, "...")
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

func exitLabel(e history.Entry) string {
	if e.ExitStatus == nil {
		return styleMuted.Render("...")
	}
	s := fmt.Sprintf("e%d", *e.ExitStatus)
	if *e.ExitStatus != 0 {
		return styleFailed.Render(s)
	}
	return styleDuration.Render(s)
}

const (
	inspectCountCol = 7
	inspectRateCol  = 4
	inspectExitCol  = 4
)
