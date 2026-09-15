package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dont-be-evil-company/remnix/internal/history"
)

func inspectUpdate(t *testing.T, m inspectModel, msg tea.Msg) inspectModel {
	t.Helper()
	got, _ := m.Update(msg)
	return got.(inspectModel)
}

func TestInspectCtrlSTogglesCriteriaPane(t *testing.T) {
	m := NewInspect(inspectFixture())
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if m.pane != paneResults {
		t.Fatalf("pane %v", m.pane)
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if m.pane != paneSidebar {
		t.Fatalf("ctrl+w should focus criteria, pane=%v", m.pane)
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.pane != paneSidebar {
		t.Fatalf("left should stay in the input, pane=%v", m.pane)
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyRight})
	if m.pane != paneSidebar {
		t.Fatalf("right should stay in the input, pane=%v", m.pane)
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if m.pane != paneResults {
		t.Fatalf("ctrl+w should return to results, pane=%v", m.pane)
	}
}

func TestInspectCtrlFTogglesAdvanced(t *testing.T) {
	m := NewInspect(inspectFixture())
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if m.view != inspectAdvanced {
		t.Fatalf("view %v", m.view)
	}
	if !strings.Contains(m.View().Content, "CRITERIA") {
		t.Fatal("expected criteria sidebar")
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if m.view != inspectOverview {
		t.Fatalf("back to overview, view=%v", m.view)
	}
}

func TestInspectStartAdvanced(t *testing.T) {
	opts := inspectFixture()
	opts.StartAdvanced = true
	m := NewInspect(opts)
	if m.view != inspectAdvanced {
		t.Fatalf("view %v", m.view)
	}
}

func TestInspectAdvancedCwdRegexFilters(t *testing.T) {
	m := NewInspect(inspectFixture())
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	m.fields[fieldCwd].SetValue("/old")
	m.refilterAdvanced()
	if len(m.visible) != 1 || m.visible[0].Command != "git status" || m.visible[0].Runs != 1 {
		t.Fatalf("cwd filter %+v", inspectCommands(m.visible))
	}
}

func TestInspectAdvancedInvalidRegexKeepsResults(t *testing.T) {
	m := NewInspect(inspectFixture())
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	m.fields[fieldCommand].SetValue("[")
	if m.refilterAdvanced() {
		t.Fatal("invalid regex should skip reload")
	}
	if m.fieldErrs[fieldCommand] == "" {
		t.Fatal("expected invalid regex error")
	}
	if len(m.visible) != 3 {
		t.Fatalf("invalid regex should keep current list, got %+v", inspectCommands(m.visible))
	}

	m.fields[fieldCommand].SetValue("git")
	if !m.refilterAdvanced() {
		t.Fatal("valid regex should reload")
	}
	if len(m.visible) != 1 || m.visible[0].Command != "git status" {
		t.Fatalf("git filter %+v", inspectCommands(m.visible))
	}
	m.fields[fieldCommand].SetValue("git status (")
	if m.refilterAdvanced() {
		t.Fatal("unclosed paren should skip reload")
	}
	if len(m.visible) != 1 || m.visible[0].Command != "git status" {
		t.Fatalf("should keep last good filter, got %+v", inspectCommands(m.visible))
	}
}

func TestInspectAdvancedEnterMatchingRunsEscBack(t *testing.T) {
	m := NewInspect(inspectFixture())
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	m.fields[fieldCwd].SetValue("/old")
	m.refilterAdvanced()
	m.snapOverview()
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.view != inspectRuns || m.runCmd != "git status" {
		t.Fatalf("view=%v cmd=%q", m.view, m.runCmd)
	}
	if len(m.runVisible) != 1 || m.runVisible[0].Cwd != "/old" {
		t.Fatalf("matching runs %+v", m.runVisible)
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.view != inspectAdvanced || m.quitting {
		t.Fatalf("esc should return to advanced: view=%v quit=%v", m.view, m.quitting)
	}
}

func TestInspectAdvancedDeletesMatchingRuns(t *testing.T) {
	opts := inspectFixture()
	var deleted []string
	opts.DeleteEntry = func(e history.Entry) error {
		deleted = append(deleted, e.ID)
		return nil
	}
	m := NewInspect(opts)
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	m.fields[fieldCwd].SetValue("/old")
	m.refilterAdvanced()
	m.snapOverview()
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: tea.KeySpace})
	if !m.cmdSelected("git status") {
		t.Fatal("expected git status selected")
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if len(deleted) != 1 || deleted[0] != "g1" {
		t.Fatalf("deleted %v", deleted)
	}
	if len(m.visible) != 0 {
		t.Fatalf("matching command should disappear, %+v", inspectCommands(m.visible))
	}
}

func TestInspectAdvancedMultiDeleteConfirms(t *testing.T) {
	opts := inspectFixture()
	var deleted []string
	opts.DeleteEntries = func(entries []history.Entry) error {
		for _, e := range entries {
			deleted = append(deleted, e.ID)
		}
		return nil
	}
	m := NewInspect(opts)
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: tea.KeySpace})
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: tea.KeySpace})
	if len(m.selectedCmds) != 2 {
		t.Fatalf("selected %d", len(m.selectedCmds))
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if m.confirmN != 2 || len(deleted) != 0 {
		t.Fatalf("expected confirm, confirmN=%d deleted=%v", m.confirmN, deleted)
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'y', Text: "y"})
	if m.confirmN != 0 {
		t.Fatalf("confirm still set %d", m.confirmN)
	}
	if len(deleted) < 2 {
		t.Fatalf("deleted %v", deleted)
	}
}

func TestInspectRunsChartKeyAndView(t *testing.T) {
	m := NewInspect(inspectFixture())
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.view != inspectRuns {
		t.Fatalf("view %v", m.view)
	}
	if m.chartGran != chartMonth {
		t.Fatalf("gran %v", m.chartGran)
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'g', Text: "g"})
	if m.chartGran != chartDay {
		t.Fatalf("gran after g %v", m.chartGran)
	}
	m = inspectUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m.View()
	if h := lipgloss.Height(v.Content); h > 24 {
		t.Fatalf("view height %d\n%s", h, v.Content)
	}
	if !strings.Contains(v.Content, "usage by") {
		t.Fatalf("expected charts\n%s", v.Content)
	}
}

func TestInspectAdvancedViewFits(t *testing.T) {
	m := NewInspect(inspectFixture())
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	m = inspectUpdate(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m.View()
	if h := lipgloss.Height(v.Content); h > 24 {
		t.Fatalf("stacked height %d\n%s", h, v.Content)
	}
	for i, line := range strings.Split(v.Content, "\n") {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line %d width %d: %q", i, lipgloss.Width(line), line)
		}
	}
	m = inspectUpdate(t, m, tea.WindowSizeMsg{Width: 120, Height: 24})
	v = m.View()
	if h := lipgloss.Height(v.Content); h > 24 {
		t.Fatalf("split height %d\n%s", h, v.Content)
	}
	if !strings.Contains(v.Content, "CRITERIA") || !strings.Contains(v.Content, "[ ADVANCED ]") {
		t.Fatalf("expected advanced chrome\n%s", v.Content)
	}
	splitLines := strings.Split(v.Content, "\n")
	if len(splitLines) < 5 || !strings.Contains(splitLines[3], "CRITERIA") {
		t.Fatalf("sidebar should start at top of body, line3=%q\n%s", splitLines[3], v.Content)
	}
	if strings.HasPrefix(strings.TrimLeft(splitLines[3], " "), "[") {
		t.Fatalf("results should sit to the right of the sidebar, line3=%q", splitLines[3])
	}
}

func TestParseInspectTimeAndExit(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	got, err := parseInspectTime("7d", now, false)
	if err != nil || !got.Equal(now.Add(-7*24*time.Hour)) {
		t.Fatalf("7d %v %v", got, err)
	}
	got, err = parseInspectTime("2026-01-02", now, false)
	if err != nil || got != time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("date %v %v", got, err)
	}
	got, err = parseInspectTime("2026-01-02", now, true)
	if err != nil || got != time.Date(2026, 1, 2, 23, 59, 59, 999000000, time.UTC) {
		t.Fatalf("until date %v %v", got, err)
	}
	failed, exact, re, err := parseExitField("!0")
	if err != nil || !failed || exact != nil || re != nil {
		t.Fatalf("failed parse %v %v %v %v", failed, exact, re, err)
	}
	failed, exact, re, err = parseExitField("0")
	if err != nil || failed || exact == nil || *exact != 0 {
		t.Fatalf("zero parse %v %v %v %v", failed, exact, re, err)
	}
	_, _, re, err = parseExitField("[0-9]")
	if err != nil || re == nil || !re.MatchString("7") {
		t.Fatalf("exit regex %v %v", re, err)
	}
}

func TestInspectAdvancedJKMovesResults(t *testing.T) {
	m := NewInspect(inspectFixture())
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if m.pane != paneResults || m.cursor != 0 {
		t.Fatalf("start pane=%v cursor=%d", m.pane, m.cursor)
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'j'})
	if m.cursor != 1 {
		t.Fatalf("j should move down, cursor=%d", m.cursor)
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'k'})
	if m.cursor != 0 {
		t.Fatalf("k should move up, cursor=%d", m.cursor)
	}

	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	if m.pane != paneSidebar {
		t.Fatalf("pane %v", m.pane)
	}
	before := m.cursor
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.cursor != before {
		t.Fatalf("j in criteria should type, cursor=%d", m.cursor)
	}
	if !strings.Contains(m.fields[fieldCommand].Value(), "j") {
		t.Fatalf("criteria got %q", m.fields[fieldCommand].Value())
	}
}

func TestInspectAdvancedSearchAfterDeleteDoesNotRelistAll(t *testing.T) {
	summaries := make([]history.CommandSummary, 80)
	for i := range summaries {
		summaries[i] = history.CommandSummary{
			Command: fmt.Sprintf("cmd-%02d", i),
			Runs:    1,
			LastTS:  time.Unix(int64(80-i), 0),
		}
	}
	var lists int
	opts := InspectOptions{
		Summaries: summaries,
		AdvancedLoad: func(_ history.SummarySort, f history.AdvancedFilter) ([]history.CommandSummary, history.Stats, error) {
			out := summaries
			if needle := f.CommandNeedle; needle != "" {
				filtered := make([]history.CommandSummary, 0)
				for _, s := range summaries {
					if strings.Contains(s.Command, needle) {
						filtered = append(filtered, s)
					}
				}
				out = filtered
			}
			return out, history.Stats{Commands: int64(len(out))}, nil
		},
		ListMatchingRuns: func(command string, _ history.AdvancedFilter) ([]history.Entry, error) {
			lists++
			return []history.Entry{{ID: command, Command: command, StartTS: time.Unix(1, 0)}}, nil
		},
		DeleteEntry: func(history.Entry) error { return nil },
	}
	m := NewInspect(opts)
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	lists = 0
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	if lists == 0 {
		t.Fatal("delete should list the selected command's runs")
	}
	if lists > 5 {
		t.Fatalf("delete listed %d commands, want a handful", lists)
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	lists = 0
	inspectUpdate(t, m, tea.KeyPressMsg{Code: 'c', Text: "c"})
	if lists != 0 {
		t.Fatalf("search after delete listed %d commands; should use AdvancedLoad only", lists)
	}
}

func TestInspectAdvancedVisualSelection(t *testing.T) {
	m := NewInspect(inspectFixture())
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	if len(m.visible) < 3 {
		t.Fatalf("need several rows, got %d", len(m.visible))
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModShift})
	if !m.visualMode || m.visualAnchor != 0 {
		t.Fatalf("visualMode=%v anchor=%d", m.visualMode, m.visualAnchor)
	}
	if got := m.selectedCommands(); len(got) != 1 || got[0] != m.visible[0].Command {
		t.Fatalf("anchor selection %+v", got)
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'j'})
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'j'})
	if !m.visualMode || m.cursor != 2 {
		t.Fatalf("cursor=%d visual=%v", m.cursor, m.visualMode)
	}
	got := m.selectedCommands()
	if len(got) != 3 {
		t.Fatalf("range size %d: %+v", len(got), got)
	}
	for i, cmd := range got {
		if cmd != m.visible[i].Command {
			t.Fatalf("range[%d]=%q want %q", i, cmd, m.visible[i].Command)
		}
	}

	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.visualMode || m.view != inspectAdvanced {
		t.Fatalf("esc should exit visual only: visual=%v view=%v", m.visualMode, m.view)
	}
	if len(m.selectedCommands()) != 3 {
		t.Fatalf("selection should remain after leaving visual: %+v", m.selectedCommands())
	}

	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.view != inspectOverview {
		t.Fatalf("second esc should leave advanced, view=%v", m.view)
	}
}

func TestInspectAdvancedVisualToggleAndIdleMove(t *testing.T) {
	m := NewInspect(inspectFixture())
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'f', Mod: tea.ModCtrl})
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModShift})
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'j'})
	if !m.visualMode || len(m.selectedCommands()) != 2 {
		t.Fatalf("visual=%v sel=%+v", m.visualMode, m.selectedCommands())
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'v', Mod: tea.ModShift})
	if m.visualMode {
		t.Fatal("shift+v should toggle visual off")
	}
	if len(m.selectedCommands()) != 2 {
		t.Fatalf("toggle off should keep selection: %+v", m.selectedCommands())
	}
	m = inspectUpdate(t, m, tea.KeyPressMsg{Code: 'j'})
	if len(m.selectedCommands()) != 2 {
		t.Fatalf("move outside visual must not change selection: %+v", m.selectedCommands())
	}
}
