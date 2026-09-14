package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dont-be-evil-company/remnix/internal/history"
)

type inspectPane int

const (
	paneResults inspectPane = iota
	paneSidebar
)

const (
	fieldCommand = iota
	fieldCwd
	fieldHost
	fieldShell
	fieldSession
	fieldDevice
	fieldExit
	fieldSince
	fieldUntil
	advancedFieldCount
)

const (
	advancedMinSplit     = 90
	advancedSidebarCols  = 32
	advancedStackedLines = 12
)

var advancedFieldNames = []string{
	"command", "cwd", "host", "shell", "session", "device", "exit", "since", "until",
}

var relativeTimeRE = regexp.MustCompile(`(?i)^(\d+)([smhdwy])$`)

func newAdvancedFields(th Theme) []textinput.Model {
	placeholders := []string{
		"regex", "regex", "regex", "regex", "regex", "regex",
		"0  !0  regex", "7d  YYYY-MM-DD", "now  YYYY-MM-DD",
	}
	fields := make([]textinput.Model, advancedFieldCount)
	for i := range fields {
		ti := textinput.New()
		ti.Prompt = ""
		ti.Placeholder = placeholders[i]
		ti.CharLimit = 256
		applyThemeInput(&ti, th)
		ti.Blur()
		fields[i] = ti
	}
	return fields
}

func (m *inspectModel) enterAdvanced() {
	if m.fields == nil {
		m.fields = newAdvancedFields(m.theme)
	}
	if q := strings.TrimSpace(m.input.Value()); q != "" && strings.TrimSpace(m.fields[fieldCommand].Value()) == "" {
		m.fields[fieldCommand].SetValue(q)
	}
	m.overviewQuery = m.input.Value()
	m.input.SetValue("")
	m.input.Blur()
	m.view = inspectAdvanced
	m.returnView = inspectAdvanced
	m.pane = paneResults
	m.fieldIdx = 0
	m.status = ""
	m.selectedCmds = map[string]struct{}{}
	m.lastFilter = history.AdvancedFilter{Sort: m.sort, Limit: 5000}
	m.blurAdvancedFields()
	m.resizeAdvancedFields()
	m.refilterAdvanced()
	m.snapOverview()
}

func (m *inspectModel) leaveAdvanced() {
	m.view = inspectOverview
	m.returnView = inspectOverview
	m.pane = paneResults
	m.selectedCmds = map[string]struct{}{}
	m.input.SetValue(m.overviewQuery)
	m.input.Placeholder = "type to search"
	_ = m.input.Focus()
	m.status = ""
	m.reloadBase()
	m.snapOverview()
}

func (m *inspectModel) blurAdvancedFields() {
	for i := range m.fields {
		m.fields[i].Blur()
	}
}

func (m *inspectModel) focusSidebarField() {
	m.blurAdvancedFields()
	if m.fieldIdx < 0 || m.fieldIdx >= len(m.fields) {
		m.fieldIdx = 0
	}
	_ = m.fields[m.fieldIdx].Focus()
}

func (m *inspectModel) setPane(p inspectPane) {
	m.pane = p
	if p == paneSidebar {
		m.input.Blur()
		m.focusSidebarField()
		return
	}
	m.blurAdvancedFields()
}

func (m *inspectModel) moveField(delta int) {
	n := len(m.fields)
	if n == 0 {
		return
	}
	m.fieldIdx = min(n-1, max(0, m.fieldIdx+delta))
	m.focusSidebarField()
}

func (m *inspectModel) resizeAdvancedFields() {
	if len(m.fields) == 0 {
		return
	}
	var w int
	if m.width >= advancedMinSplit {
		w = advancedSidebarCols - 12
	} else {
		w = max(8, m.width-16)
	}
	for i := range m.fields {
		m.fields[i].SetWidth(max(8, w))
	}
}

func (m *inspectModel) compiledFilter() history.AdvancedFilter {
	f, ok := m.parseAdvancedFilter()
	if !ok {
		f := m.lastFilter
		f.Sort = m.sort
		if f.Limit == 0 {
			f.Limit = 5000
		}
		return f
	}
	m.lastFilter = f
	return f
}

func (m *inspectModel) parseAdvancedFilter() (history.AdvancedFilter, bool) {
	f := history.AdvancedFilter{Sort: m.sort, Limit: 5000}
	if len(m.fields) == 0 {
		return f, true
	}
	if m.fieldErrs == nil || len(m.fieldErrs) != advancedFieldCount {
		m.fieldErrs = make([]string, advancedFieldCount)
	} else {
		for i := range m.fieldErrs {
			m.fieldErrs[i] = ""
		}
	}
	ok := true
	val := func(i int) string { return strings.TrimSpace(m.fields[i].Value()) }
	compile := func(i int) *regexp.Regexp {
		s := val(i)
		if s == "" {
			return nil
		}
		re, err := regexp.Compile(s)
		if err != nil {
			m.fieldErrs[i] = "invalid regex"
			ok = false
			return nil
		}
		return re
	}
	if s := val(fieldCommand); s != "" {
		f.CommandRE = compile(fieldCommand)
		if f.CommandRE != nil {
			f.CommandNeedle = history.RegexLiteralPrefix(s)
		}
	}
	f.CwdRE = compile(fieldCwd)
	f.HostRE = compile(fieldHost)
	f.ShellRE = compile(fieldShell)
	f.SessionRE = compile(fieldSession)
	f.DeviceRE = compile(fieldDevice)
	if s := val(fieldExit); s != "" {
		failed, exact, re, err := parseExitField(s)
		if err != nil {
			m.fieldErrs[fieldExit] = "invalid"
			ok = false
		} else {
			f.ExitFailed = failed
			f.Exit = exact
			f.ExitRE = re
		}
	}
	now := time.Now().UTC()
	if s := val(fieldSince); s != "" {
		t, err := parseInspectTime(s, now, false)
		if err != nil {
			m.fieldErrs[fieldSince] = "invalid time"
			ok = false
		} else {
			f.Since = &t
		}
	}
	if s := val(fieldUntil); s != "" {
		t, err := parseInspectTime(s, now, true)
		if err != nil {
			m.fieldErrs[fieldUntil] = "invalid time"
			ok = false
		} else {
			f.Until = &t
		}
	}
	return f, ok
}

func parseExitField(s string) (failed bool, exact *int, re *regexp.Regexp, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return false, nil, nil, nil
	}
	switch strings.ToLower(s) {
	case "failed", "fail", "!0":
		return true, nil, nil, nil
	}
	if n, convErr := strconv.Atoi(s); convErr == nil {
		return false, &n, nil, nil
	}
	re, err = regexp.Compile(s)
	return false, nil, re, err
}

func parseInspectTime(s string, now time.Time, until bool) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	if strings.EqualFold(s, "now") {
		return now.UTC(), nil
	}
	if m := relativeTimeRE.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		var d time.Duration
		switch strings.ToLower(m[2]) {
		case "s":
			d = time.Duration(n) * time.Second
		case "m":
			d = time.Duration(n) * time.Minute
		case "h":
			d = time.Duration(n) * time.Hour
		case "d":
			d = time.Duration(n) * 24 * time.Hour
		case "w":
			d = time.Duration(n) * 7 * 24 * time.Hour
		case "y":
			d = time.Duration(n) * 365 * 24 * time.Hour
		}
		return now.UTC().Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04", s, time.UTC); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.UTC); err == nil {
		if until {
			return t.Add(24*time.Hour - time.Millisecond), nil
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid time")
}

func (m *inspectModel) refilterAdvanced() bool {
	f, ok := m.parseAdvancedFilter()
	if !ok {
		return false
	}
	m.lastFilter = f
	if m.advancedLoad != nil {
		all, st, err := m.advancedLoad(m.sort, f)
		if err != nil {
			m.status = err.Error()
			return false
		}
		// Store queries already omit tombstones. Re-listing every command's
		// matching runs here made search O(n) SQL after the first delete.
		m.all = all
		m.stats = st
		m.visible = filterSummaries(m.all, "", m.sort)
		m.clampCursor()
		m.pruneSelectedCmds()
		return true
	}
	out := make([]history.CommandSummary, 0, len(m.all))
	for _, s := range m.all {
		runs := m.matchingRuns(s.Command, f)
		if len(runs) == 0 {
			continue
		}
		out = append(out, history.SummarizeRuns(s.Command, runs))
	}
	m.visible = filterSummaries(out, "", m.sort)
	m.clampCursor()
	m.pruneSelectedCmds()
	return true
}

func (m *inspectModel) matchingRuns(command string, f history.AdvancedFilter) []history.Entry {
	var runs []history.Entry
	var err error
	if m.listMatchingRuns != nil {
		runs, err = m.listMatchingRuns(command, f)
	} else if m.listRuns != nil {
		runs, err = m.listRuns(command)
		if err == nil {
			runs = history.FilterEntries(runs, f)
		}
	}
	if err != nil {
		return nil
	}
	return m.rejectDeleted(runs)
}

func (m inspectModel) rejectDeleted(runs []history.Entry) []history.Entry {
	if len(m.deletedIDs) == 0 {
		return runs
	}
	out := make([]history.Entry, 0, len(runs))
	for _, e := range runs {
		if _, ok := m.deletedIDs[e.ID]; ok {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (m inspectModel) cmdSelected(command string) bool {
	_, ok := m.selectedCmds[command]
	return ok
}

func (m inspectModel) runSelected(id string) bool {
	_, ok := m.selectedRuns[id]
	return ok
}

func (m *inspectModel) toggleSelected() {
	if m.view == inspectRuns {
		if len(m.runVisible) == 0 || m.runCursor >= len(m.runVisible) {
			return
		}
		if m.selectedRuns == nil {
			m.selectedRuns = map[string]struct{}{}
		}
		id := m.runVisible[m.runCursor].ID
		if _, ok := m.selectedRuns[id]; ok {
			delete(m.selectedRuns, id)
		} else {
			m.selectedRuns[id] = struct{}{}
		}
		return
	}
	if m.view != inspectAdvanced {
		return
	}
	if len(m.visible) == 0 || m.cursor >= len(m.visible) {
		return
	}
	if m.selectedCmds == nil {
		m.selectedCmds = map[string]struct{}{}
	}
	cmd := m.visible[m.cursor].Command
	if _, ok := m.selectedCmds[cmd]; ok {
		delete(m.selectedCmds, cmd)
	} else {
		m.selectedCmds[cmd] = struct{}{}
	}
}

func (m *inspectModel) toggleSelectAll() {
	if m.view == inspectRuns {
		if m.selectedRuns == nil {
			m.selectedRuns = map[string]struct{}{}
		}
		if len(m.selectedRuns) == len(m.runVisible) && len(m.runVisible) > 0 {
			m.selectedRuns = map[string]struct{}{}
			return
		}
		for _, e := range m.runVisible {
			m.selectedRuns[e.ID] = struct{}{}
		}
		return
	}
	if m.view != inspectAdvanced {
		return
	}
	if m.selectedCmds == nil {
		m.selectedCmds = map[string]struct{}{}
	}
	if len(m.selectedCmds) == len(m.visible) && len(m.visible) > 0 {
		m.selectedCmds = map[string]struct{}{}
		return
	}
	for _, s := range m.visible {
		m.selectedCmds[s.Command] = struct{}{}
	}
}

func (m *inspectModel) pruneSelectedCmds() {
	if len(m.selectedCmds) == 0 {
		return
	}
	keep := make(map[string]struct{}, len(m.selectedCmds))
	for _, s := range m.visible {
		if _, ok := m.selectedCmds[s.Command]; ok {
			keep[s.Command] = struct{}{}
		}
	}
	m.selectedCmds = keep
}

func (m *inspectModel) pruneSelectedRuns() {
	if len(m.selectedRuns) == 0 {
		return
	}
	keep := make(map[string]struct{}, len(m.selectedRuns))
	for _, e := range m.runVisible {
		if _, ok := m.selectedRuns[e.ID]; ok {
			keep[e.ID] = struct{}{}
		}
	}
	m.selectedRuns = keep
}

func (m inspectModel) selectedCommands() []string {
	if len(m.visible) == 0 {
		return nil
	}
	if len(m.selectedCmds) > 0 {
		out := make([]string, 0, len(m.selectedCmds))
		for _, s := range m.visible {
			if _, ok := m.selectedCmds[s.Command]; ok {
				out = append(out, s.Command)
			}
		}
		return out
	}
	if m.cursor >= len(m.visible) {
		return nil
	}
	return []string{m.visible[m.cursor].Command}
}

func (m inspectModel) selectedRunEntries() []history.Entry {
	if len(m.runVisible) == 0 {
		return nil
	}
	if len(m.selectedRuns) > 0 {
		out := make([]history.Entry, 0, len(m.selectedRuns))
		for _, e := range m.runVisible {
			if _, ok := m.selectedRuns[e.ID]; ok {
				out = append(out, e)
			}
		}
		return out
	}
	if m.runCursor >= len(m.runVisible) {
		return nil
	}
	return []history.Entry{m.runVisible[m.runCursor]}
}

func (m *inspectModel) requestDelete() {
	switch m.view {
	case inspectRuns:
		entries := m.selectedRunEntries()
		if len(entries) == 0 {
			return
		}
		if len(entries) > 1 {
			m.confirmN = len(entries)
			m.confirmEntries = entries
			m.confirmKind = "runs"
			return
		}
		m.deleteEntriesNow(entries)
	case inspectAdvanced:
		cmds := m.selectedCommands()
		if len(cmds) == 0 {
			return
		}
		f := m.compiledFilter()
		var entries []history.Entry
		for _, cmd := range cmds {
			entries = append(entries, m.matchingRuns(cmd, f)...)
		}
		if len(entries) == 0 {
			return
		}
		if len(cmds) > 1 {
			m.confirmN = len(cmds)
			m.confirmEntries = entries
			m.confirmKind = "commands"
			return
		}
		m.deleteEntriesNow(entries)
	default:
		m.deleteSelectedCommand()
	}
}

func (m inspectModel) handleConfirm(msg tea.KeyPressMsg) (inspectModel, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "y", "Y":
		entries := m.confirmEntries
		m.clearConfirm()
		m.deleteEntriesNow(entries)
		return m, nil
	case "n", "N", "esc":
		m.clearConfirm()
		return m, nil
	}
	return m, nil
}

func (m *inspectModel) clearConfirm() {
	m.confirmN = 0
	m.confirmEntries = nil
	m.confirmKind = ""
}

func (m *inspectModel) deleteEntriesNow(entries []history.Entry) {
	if len(entries) == 0 {
		return
	}
	if m.deleteEntries != nil {
		if err := m.deleteEntries(entries); err != nil {
			m.status = err.Error()
			return
		}
	} else if m.deleteEntry != nil {
		for _, e := range entries {
			if err := m.deleteEntry(e); err != nil {
				m.status = err.Error()
				return
			}
		}
	}
	if m.deletedIDs == nil {
		m.deletedIDs = map[string]struct{}{}
	}
	ids := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		ids[e.ID] = struct{}{}
		m.deletedIDs[e.ID] = struct{}{}
		m.noteDeletedRun(e)
	}
	m.status = ""
	m.clearConfirm()
	if m.view == inspectRuns {
		m.runs = m.rejectDeleted(m.runs)
		if m.load != nil || m.advancedLoad != nil {
			if runs, err := m.loadCurrentRuns(m.runCmd); err != nil {
				m.status = err.Error()
			} else {
				m.runs = runs
			}
		}
		for _, id := range mapsKeys(ids) {
			delete(m.selectedRuns, id)
		}
		if len(m.runs) == 0 {
			if m.load == nil && m.advancedLoad == nil {
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
		m.runSummary = history.SummarizeRuns(m.runCmd, m.runs)
		m.refilterRuns()
		m.pruneSelectedRuns()
		m.clampRunCursor()
		return
	}
	if m.view == inspectAdvanced {
		m.selectedCmds = map[string]struct{}{}
		m.refilterAdvanced()
		return
	}
	m.reloadBase()
}

func mapsKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (m *inspectModel) noteDeletedRun(e history.Entry) {
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
}

func (m *inspectModel) loadCurrentRuns(command string) ([]history.Entry, error) {
	fromAdvanced := m.view == inspectAdvanced || m.returnView == inspectAdvanced
	if fromAdvanced {
		return m.matchingRuns(command, m.compiledFilter()), nil
	}
	if m.listRuns == nil {
		return nil, nil
	}
	runs, err := m.listRuns(command)
	if err != nil {
		return nil, err
	}
	return m.rejectDeleted(runs), nil
}

func (m inspectModel) viewAdvanced() tea.View {
	w := max(1, m.width)
	h := max(1, m.height)
	inner := max(1, w-1)

	header := clampLine(m.renderInspectHeader(inner), inner)
	stats := clampLine(m.renderInspectStats(), inner)
	detail := clampLine(m.renderInspectDetail(), inner)
	help := clampLine(m.renderInspectHelp(), inner)
	input := clampLine(m.renderInspectInput(), inner)
	rule := m.theme.Rule.Render(strings.Repeat("─", inner))

	chrome := lipgloss.Height(header) + lipgloss.Height(stats) + 1 + lipgloss.Height(detail) + lipgloss.Height(help) + lipgloss.Height(input)
	bodyH := h - chrome
	if bodyH < 0 {
		bodyH = 0
	}

	var body string
	if inner >= advancedMinSplit && bodyH > 0 {
		sideW := advancedSidebarCols
		listW := max(8, inner-sideW-1)
		sep := m.theme.Rule.Render("│")
		sideLines := padBlock(m.renderSidebar(sideW, bodyH), sideW, bodyH, false)
		listLines := padBlock(m.renderInspectList(listW, bodyH), listW, bodyH, false)
		var sb strings.Builder
		for i := 0; i < bodyH; i++ {
			if i > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(sideLines[i])
			sb.WriteString(sep)
			sb.WriteString(listLines[i])
		}
		body = sb.String()
	} else {
		sideH := min(advancedStackedLines, max(0, bodyH/2))
		listH := max(0, bodyH-sideH)
		var b strings.Builder
		if sideH > 0 {
			b.WriteString(strings.Join(padBlock(m.renderSidebar(inner, sideH), inner, sideH, false), "\n"))
			b.WriteByte('\n')
		}
		if listH > 0 {
			b.WriteString(m.renderInspectList(inner, listH))
		}
		body = b.String()
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	b.WriteString(stats)
	b.WriteByte('\n')
	b.WriteString(rule)
	b.WriteByte('\n')
	if bodyH > 0 {
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
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

func padBlock(s string, w, h int, bottomAlign bool) []string {
	if h <= 0 {
		return nil
	}
	s = strings.TrimRight(s, "\n")
	var raw []string
	if s != "" {
		raw = strings.Split(s, "\n")
	}
	lines := make([]string, 0, max(h, len(raw)))
	for _, line := range raw {
		lines = append(lines, padWidth(clampLine(line, w), w))
	}
	if len(lines) > h {
		if bottomAlign {
			lines = lines[len(lines)-h:]
		} else {
			lines = lines[:h]
		}
	}
	blank := strings.Repeat(" ", w)
	for len(lines) < h {
		if bottomAlign {
			lines = append([]string{blank}, lines...)
		} else {
			lines = append(lines, blank)
		}
	}
	return lines
}

func padWidth(s string, w int) string {
	n := lipgloss.Width(s)
	if n >= w {
		return s
	}
	return s + strings.Repeat(" ", w-n)
}

func (m inspectModel) renderSidebar(w, h int) string {
	if h <= 0 {
		return ""
	}
	var b strings.Builder
	title := "CRITERIA"
	if m.pane == paneSidebar {
		title = m.theme.Accent.Render(title)
	} else {
		title = m.theme.Muted.Render(title)
	}
	b.WriteString(clampLine(title, w))
	used := 1
	if used < h {
		b.WriteByte('\n')
		b.WriteString(m.theme.Rule.Render(strings.Repeat("─", max(1, w))))
		used++
	}
	for i, name := range advancedFieldNames {
		if used >= h {
			break
		}
		b.WriteByte('\n')
		b.WriteString(clampLine(m.renderSidebarField(i, name, w), w))
		used++
	}
	return b.String()
}

func (m inspectModel) renderSidebarField(i int, name string, w int) string {
	label := fmt.Sprintf("%-8s", name)
	if i == m.fieldIdx && m.pane == paneSidebar {
		label = m.theme.Accent.Render(label)
	} else {
		label = m.theme.Muted.Render(label)
	}
	val := ""
	if i < len(m.fields) {
		val = m.fields[i].View()
	}
	line := label + " " + val
	if i < len(m.fieldErrs) && m.fieldErrs[i] != "" {
		errS := m.theme.Failed.Render(" " + m.fieldErrs[i])
		remain := w - lipgloss.Width(line)
		if remain > lipgloss.Width(errS) {
			line += errS
		}
	}
	return line
}

func (m inspectModel) renderConfirmHelp() string {
	noun := "matching runs"
	switch m.confirmKind {
	case "commands":
		noun = "commands' matching runs"
	case "runs":
		noun = "runs"
	}
	return m.theme.Failed.Render(fmt.Sprintf("delete %d %s? y/n", m.confirmN, noun))
}
