package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mistweaverco/syncsh/internal/history"
)

func inspectFixture() InspectOptions {
	ok := 0
	fail := 1
	summaries := []history.CommandSummary{
		{
			Command: "make test", Runs: 1, Success: 0, Failed: 1,
			LastTS: time.Unix(30, 0), LastExit: &fail, LastCwd: "/src", LastHost: "dev",
		},
		{
			Command: "git status", Runs: 3, Success: 2, Failed: 1,
			FirstTS: time.Unix(1, 0), LastTS: time.Unix(20, 0), LastExit: &ok,
			LastCwd: "/repo", LastHost: "dev",
		},
		{
			Command: "ls", Runs: 10, Success: 10, Failed: 0,
			LastTS: time.Unix(10, 0), LastExit: &ok, LastCwd: "/tmp", LastHost: "dev",
		},
	}
	runs := map[string][]history.Entry{
		"make test": {
			{ID: "m1", Command: "make test", StartTS: time.Unix(30, 0), Cwd: "/src", Hostname: "dev", ExitStatus: &fail, DeviceID: "d", Shell: "zsh"},
		},
		"git status": {
			{ID: "g2", Command: "git status", StartTS: time.Unix(20, 0), Cwd: "/repo", Hostname: "dev", ExitStatus: &ok, SessionID: "s1", DeviceID: "d", Shell: "zsh"},
			{ID: "g1", Command: "git status", StartTS: time.Unix(5, 0), Cwd: "/old", Hostname: "laptop", ExitStatus: &fail, DeviceID: "d", Shell: "zsh"},
		},
		"ls": {
			{ID: "l1", Command: "ls", StartTS: time.Unix(10, 0), Cwd: "/tmp", Hostname: "dev", ExitStatus: &ok},
		},
	}
	return InspectOptions{
		Stats:     history.Stats{Commands: 14, Deleted: 1, Success: 12, Failed: 2, Devices: 2, Sessions: 4},
		Summaries: summaries,
		ListRuns: func(command string) ([]history.Entry, error) {
			return append([]history.Entry(nil), runs[command]...), nil
		},
	}
}

func TestInspectTabCyclesSort(t *testing.T) {
	m := NewInspect(inspectFixture())
	if m.sort != history.SortRecent {
		t.Fatalf("recent sort %v", m.sort)
	}
	if last := m.visible[m.cursor].Command; last != "make test" || m.cursor != len(m.visible)-1 {
		t.Fatalf("recent newest at bottom: cursor=%d cmd=%q", m.cursor, last)
	}
	if m.visible[0].Command != "ls" {
		t.Fatalf("oldest at top: %q", m.visible[0].Command)
	}
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = got.(inspectModel)
	if m.sort != history.SortTop || m.visible[m.cursor].Command != "ls" {
		t.Fatalf("top: sort=%v cmd=%q", m.sort, m.visible[m.cursor].Command)
	}
	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = got.(inspectModel)
	if m.sort != history.SortFailed {
		t.Fatalf("failed sort %v", m.sort)
	}
	if len(m.visible) != 2 || m.visible[len(m.visible)-1].Command != "make test" || m.visible[0].Command != "git status" {
		t.Fatalf("failed list %+v", inspectCommands(m.visible))
	}
	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = got.(inspectModel)
	if m.sort != history.SortRecent || m.visible[m.cursor].Command != "make test" {
		t.Fatalf("back to recent: sort=%v cmd=%q", m.sort, m.visible[m.cursor].Command)
	}
}

func TestInspectSearchFilters(t *testing.T) {
	m := NewInspect(inspectFixture())
	got, _ := m.Update(tea.KeyPressMsg{Code: 'g', Text: "g"})
	m = got.(inspectModel)
	if m.input.Value() != "g" {
		t.Fatalf("query %q", m.input.Value())
	}
	if len(m.visible) != 1 || m.visible[0].Command != "git status" {
		t.Fatalf("filtered %+v", inspectCommands(m.visible))
	}
}

func TestInspectEnterDrillDownEscBack(t *testing.T) {
	m := NewInspect(inspectFixture())
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = got.(inspectModel)
	if m.view != inspectRuns || m.runCmd != "make test" {
		t.Fatalf("view=%v cmd=%q", m.view, m.runCmd)
	}
	if len(m.runVisible) != 1 || m.runVisible[0].ID != "m1" {
		t.Fatalf("runs %+v", m.runVisible)
	}
	if m.quitting {
		t.Fatal("enter should not quit")
	}
	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = got.(inspectModel)
	if m.view != inspectOverview || m.quitting {
		t.Fatalf("esc should go back: view=%v quit=%v", m.view, m.quitting)
	}
	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = got.(inspectModel)
	if !m.quitting {
		t.Fatal("second esc should quit")
	}
}

func TestInspectCtrlDDeletesCommand(t *testing.T) {
	opts := inspectFixture()
	var deleted []string
	opts.DeleteCommand = func(command string) error {
		deleted = append(deleted, command)
		return nil
	}
	m := NewInspect(opts)
	got, _ := m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	m = got.(inspectModel)
	if len(deleted) != 1 || deleted[0] != "make test" {
		t.Fatalf("deleted %v", deleted)
	}
	if m.quitting || m.view != inspectOverview {
		t.Fatal("delete should stay in overview")
	}
	for _, s := range m.visible {
		if s.Command == "make test" {
			t.Fatal("command still listed")
		}
	}
	if m.stats.Commands != 13 {
		t.Fatalf("commands stat %d", m.stats.Commands)
	}
}

func TestInspectCtrlDDeletesRunAndPopsWhenEmpty(t *testing.T) {
	opts := inspectFixture()
	var deleted []string
	opts.DeleteEntry = func(e history.Entry) error {
		deleted = append(deleted, e.ID)
		return nil
	}
	m := NewInspect(opts)
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = got.(inspectModel)
	got, _ = m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	m = got.(inspectModel)
	if len(deleted) != 1 || deleted[0] != "m1" {
		t.Fatalf("deleted %v", deleted)
	}
	if m.view != inspectOverview {
		t.Fatalf("last run should pop back, view=%v", m.view)
	}
	for _, s := range m.visible {
		if s.Command == "make test" {
			t.Fatal("empty command still listed")
		}
	}
}

func TestInspectCtrlDKeepsRowOnError(t *testing.T) {
	opts := inspectFixture()
	opts.DeleteCommand = func(string) error { return errors.New("nope") }
	m := NewInspect(opts)
	got, _ := m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	m = got.(inspectModel)
	if len(m.visible) != 3 {
		t.Fatalf("visible %d", len(m.visible))
	}
	if m.status == "" {
		t.Fatal("expected status")
	}
}

func TestInspectRunSearchFiltersCwd(t *testing.T) {
	m := NewInspect(inspectFixture())
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = got.(inspectModel)
	if m.visible[m.cursor].Command != "git status" {
		t.Fatalf("cursor %q", m.visible[m.cursor].Command)
	}
	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = got.(inspectModel)
	if len(m.runVisible) != 2 {
		t.Fatalf("runs %d", len(m.runVisible))
	}
	for _, r := range "old" {
		got, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		m = got.(inspectModel)
	}
	if len(m.runVisible) != 1 || m.runVisible[0].Cwd != "/old" {
		t.Fatalf("cwd filter %+v", m.runVisible)
	}
}

func TestInspectViewKeepsInputOnScreen(t *testing.T) {
	summaries := make([]history.CommandSummary, 80)
	for i := range summaries {
		summaries[i] = history.CommandSummary{
			Command: strings.Repeat("echo lots of results ", 8) + fmt.Sprint(i),
			Runs:    int64(i + 1),
			LastTS:  time.Unix(int64(i+1), 0),
		}
	}
	m := NewInspect(InspectOptions{Summaries: summaries, Stats: history.Stats{Commands: 80}})
	got, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = got.(inspectModel)
	v := m.View()
	if h := lipgloss.Height(v.Content); h > 24 {
		t.Fatalf("view height %d exceeds terminal 24\n%s", h, v.Content)
	}
	lines := strings.Split(v.Content, "\n")
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "[ RECENT ]") {
		t.Fatalf("input should be the last line, got %q", lines[len(lines)-1])
	}
	if !strings.Contains(v.Content, "commands:") {
		t.Fatal("expected stats in header")
	}
	for i, line := range lines {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line %d width %d > 80: %q", i, lipgloss.Width(line), line)
		}
	}
}

func inspectCommands(in []history.CommandSummary) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = s.Command
	}
	return out
}
