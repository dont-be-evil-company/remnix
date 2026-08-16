package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/dont-be-evil-company/remnix/internal/config"
	"github.com/dont-be-evil-company/remnix/internal/history"
)

func TestFilterCwdAndQuery(t *testing.T) {
	entries := []history.Entry{
		{Command: "git status", Cwd: "/a", StartTS: time.Unix(3, 0)},
		{Command: "ls", Cwd: "/b", StartTS: time.Unix(2, 0)},
		{Command: "git push", Cwd: "/a", StartTS: time.Unix(1, 0)},
	}
	got := Filter(entries, "git", "/a", true)
	if len(got) != 2 {
		t.Fatalf("got %d", len(got))
	}
	all := Filter(entries, "git", "/a", false)
	if len(all) != 2 {
		t.Fatalf("all git %d", len(all))
	}
	empty := Filter(entries, "", "/a", true)
	if len(empty) != 2 || empty[0].Command != "git status" {
		t.Fatalf("cwd only newest-first: %+v", empty)
	}
}

func TestSearchKeepsKeysAfterFocusBlur(t *testing.T) {
	m := New([]history.Entry{{Command: "echo hi", StartTS: time.Unix(1, 0)}}, Options{})
	got, _ := m.Update(tea.BlurMsg{})
	got, _ = got.(model).Update(tea.FocusMsg{})
	got, _ = got.(model).Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if !strings.Contains(got.(model).input.Value(), "x") {
		t.Fatalf("typed x after blur/focus, query=%q", got.(model).input.Value())
	}
}

func TestEnterAcceptsEscCancels(t *testing.T) {
	m := New([]history.Entry{{Command: "echo hi", StartTS: time.Unix(1, 0)}}, Options{})
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	gm := got.(model)
	if cmd, ok := gm.SelectedCommand(); !ok || cmd != "echo hi" {
		t.Fatalf("accept: %q ok=%v", cmd, ok)
	}
	if !gm.RunSelected() {
		t.Fatal("enter should run")
	}

	m = New([]history.Entry{{Command: "echo hi"}}, Options{})
	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	gm = got.(model)
	if _, ok := gm.SelectedCommand(); ok {
		t.Fatal("esc should not print")
	}
}

func TestCtrlOInsertsWithoutRunning(t *testing.T) {
	m := New([]history.Entry{{Command: "echo hi", StartTS: time.Unix(1, 0)}}, Options{})
	got, _ := m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	gm := got.(model)
	cmd, ok := gm.SelectedCommand()
	if !ok || cmd != "echo hi" {
		t.Fatalf("insert: %q ok=%v", cmd, ok)
	}
	if gm.RunSelected() {
		t.Fatal("ctrl+o should not run")
	}
}

func TestTabTogglesCwd(t *testing.T) {
	m := New([]history.Entry{
		{Command: "here", Cwd: "/a", StartTS: time.Unix(2, 0)},
		{Command: "there", Cwd: "/b", StartTS: time.Unix(1, 0)},
	}, Options{Cwd: "/a"})
	if len(m.visible) != 2 {
		t.Fatalf("start %d", len(m.visible))
	}
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	gm := got.(model)
	if !gm.cwdOnly || len(gm.visible) != 1 || gm.visible[0].Command != "here" {
		t.Fatalf("cwd filter: only=%v n=%d", gm.cwdOnly, len(gm.visible))
	}
}

func TestListNewestSitsAtBottom(t *testing.T) {
	m := New([]history.Entry{
		{Command: "old", StartTS: time.Unix(1, 0)},
		{Command: "new", StartTS: time.Unix(9, 0)},
		{Command: "mid", StartTS: time.Unix(5, 0)},
	}, Options{})
	if m.cursor != 2 || m.visible[m.cursor].Command != "new" {
		t.Fatalf("cursor=%d cmd=%q", m.cursor, m.visible[m.cursor].Command)
	}
	if m.visible[0].Command != "old" {
		t.Fatalf("oldest should be at top, got %q", m.visible[0].Command)
	}
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	gm := got.(model)
	if gm.visible[gm.cursor].Command != "mid" {
		t.Fatalf("up should move to older: %q", gm.visible[gm.cursor].Command)
	}
	got, _ = gm.Update(tea.KeyReleaseMsg{Code: tea.KeyUp})
	gm = got.(model)
	if gm.visible[gm.cursor].Command != "mid" {
		t.Fatalf("key release should not snap cursor back, got %q", gm.visible[gm.cursor].Command)
	}
}

func TestTypingSnapsCursorToBestMatch(t *testing.T) {
	m := New([]history.Entry{
		{Command: "old", StartTS: time.Unix(1, 0)},
		{Command: "new", StartTS: time.Unix(9, 0)},
		{Command: "mid", StartTS: time.Unix(5, 0)},
	}, Options{})
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	gm := got.(model)
	got, _ = gm.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	gm = got.(model)
	if gm.input.Value() != "o" {
		t.Fatalf("query %q", gm.input.Value())
	}
	if gm.visible[gm.cursor].Command != "old" {
		t.Fatalf("typing should snap to best match, got %q", gm.visible[gm.cursor].Command)
	}
}

func TestHighlightCommand(t *testing.T) {
	s := HighlightCommand(`git pull --rebase && echo "ok" # done`)
	if !strings.Contains(s, "\x1b") {
		t.Fatalf("expected ANSI styling, got %q", s)
	}
	if !strings.Contains(s, "git") || !strings.Contains(s, "--rebase") {
		t.Fatalf("lost command text: %q", s)
	}
	plain := HighlightCommand("ls")
	if plain == "ls" {
		t.Fatal("command should be styled")
	}
}

func TestFormatDurationAndRelative(t *testing.T) {
	ms := int64(25000)
	if got := formatDuration(&ms); got != "25s" {
		t.Fatalf("duration %q", got)
	}
	now := time.Unix(1000, 0)
	if got := formatRelative(time.Unix(1000-8*3600, 0), now); got != "8h ago" {
		t.Fatalf("relative %q", got)
	}
	if got := formatRelative(time.Unix(1000-32, 0), now); got != "32s ago" {
		t.Fatalf("seconds %q", got)
	}
	if got := formatRelative(now, now); got != "now" {
		t.Fatalf("now %q", got)
	}
}

func TestRenderRowColumnsAlign(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	msLong := int64(167)
	msShort := int64(1000)
	m := New(nil, Options{})
	a := m.renderRow(history.Entry{Command: "gcloud auth login", DurationMs: &msLong, StartTS: now.Add(-time.Minute)}, false, 80, now)
	b := m.renderRow(history.Entry{Command: "make build", DurationMs: &msShort, StartTS: now.Add(-32 * time.Second)}, true, 80, now)
	// Command text starts after a fixed prefix width on every row.
	wa := lipgloss.Width(a[:strings.Index(a, "gcloud")])
	wb := lipgloss.Width(b[:strings.Index(b, "make")])
	if wa != wb {
		t.Fatalf("command column starts at %d vs %d\n%q\n%q", wa, wb, a, b)
	}
}

func TestCtrlDDeletesSelected(t *testing.T) {
	var deleted []string
	m := New([]history.Entry{
		{ID: "1", Command: "old", StartTS: time.Unix(1, 0)},
		{ID: "2", Command: "secret", StartTS: time.Unix(5, 0)},
		{ID: "3", Command: "new", StartTS: time.Unix(9, 0)},
	}, Options{Delete: func(e history.Entry) error {
		deleted = append(deleted, e.Command)
		return nil
	}})
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	gm := got.(model)
	if gm.visible[gm.cursor].Command != "secret" {
		t.Fatalf("want secret under cursor, got %q", gm.visible[gm.cursor].Command)
	}
	got, _ = gm.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	gm = got.(model)
	if len(deleted) != 1 || deleted[0] != "secret" {
		t.Fatalf("deleted %v", deleted)
	}
	if gm.quitting {
		t.Fatal("delete should not quit")
	}
	if len(gm.visible) != 2 {
		t.Fatalf("visible %d", len(gm.visible))
	}
	for _, e := range gm.visible {
		if e.Command == "secret" {
			t.Fatal("secret still listed")
		}
	}
	if gm.visible[gm.cursor].Command != "new" {
		t.Fatalf("cursor should stay on next newer row, got %q", gm.visible[gm.cursor].Command)
	}
}

func TestCtrlDKeepsRowOnError(t *testing.T) {
	m := New([]history.Entry{{ID: "1", Command: "keep me", StartTS: time.Unix(1, 0)}}, Options{
		Delete: func(history.Entry) error { return errors.New("nope") },
	})
	got, _ := m.Update(tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl})
	gm := got.(model)
	if len(gm.visible) != 1 {
		t.Fatal("row should remain on error")
	}
	if gm.status == "" {
		t.Fatal("expected status")
	}
}

func TestViewKeepsInputOnScreen(t *testing.T) {
	entries := make([]history.Entry, 80)
	for i := range entries {
		entries[i] = history.Entry{
			Command: strings.Repeat("echo lots of results ", 8) + fmt.Sprint(i),
			StartTS: time.Unix(int64(i+1), 0),
		}
	}
	m := New(entries, Options{Cwd: "/very/long/path/" + strings.Repeat("dir/", 20)})
	got, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = got.(model)
	v := m.View()
	if h := lipgloss.Height(v.Content); h > 24 {
		t.Fatalf("view height %d exceeds terminal 24\n%s", h, v.Content)
	}
	lines := strings.Split(v.Content, "\n")
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "[ ALL ]") {
		t.Fatalf("input should be the last line, got %q", lines[len(lines)-1])
	}
	for i, line := range lines {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line %d width %d > 80: %q", i, lipgloss.Width(line), line)
		}
	}

	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = got.(model)
	v = m.View()
	if h := lipgloss.Height(v.Content); h > 24 {
		t.Fatalf("cwd view height %d exceeds 24", h)
	}
	lines = strings.Split(v.Content, "\n")
	if !strings.Contains(lines[len(lines)-1], "[ DIR ]") {
		t.Fatalf("input should stay last after tab, got %q", lines[len(lines)-1])
	}
}

func TestWriteSelectionWidgetRuns(t *testing.T) {
	var buf strings.Builder
	WriteSelection(&buf, "echo hi", true)
	if buf.String() != AcceptPrefix+"echo hi\n" {
		t.Fatalf("got %q", buf.String())
	}
	buf.Reset()
	WriteSelection(&buf, "echo hi", false)
	if buf.String() != "echo hi\n" {
		t.Fatalf("browse %q", buf.String())
	}
	if FormatSelection("echo hi", true) != AcceptPrefix+"echo hi" {
		t.Fatal("FormatSelection must not add a newline")
	}
	if FormatSelection("echo hi", false) != "echo hi" {
		t.Fatal("FormatSelection browse")
	}
}

func TestNewKeepsPassedTheme(t *testing.T) {
	th := NewTheme(config.UI{Colors: config.Colors{Title: "#ff0000"}})
	m := New(nil, Options{Theme: th})
	if m.theme.Colors.Title != "#ff0000" {
		t.Fatalf("theme discarded: %+v", m.theme.Colors)
	}
}

func TestSearchUsesConfigTheme(t *testing.T) {
	def := New([]history.Entry{{Command: "ls", StartTS: time.Unix(1, 0)}}, Options{})
	custom := New([]history.Entry{{Command: "ls", StartTS: time.Unix(1, 0)}}, Options{
		Theme: NewTheme(config.UI{Colors: config.Colors{
			Title:  "#ff0000",
			Muted:  "#888888",
			Accent: "#00ff00",
		}}),
	})
	if def.renderHeader(40) == custom.renderHeader(40) {
		t.Fatal("custom title color should change the header")
	}
	now := time.Unix(1, 0)
	if def.renderRow(history.Entry{Command: "ls", StartTS: now}, true, 80, now) ==
		custom.renderRow(history.Entry{Command: "ls", StartTS: now}, true, 80, now) {
		t.Fatal("custom accent color should change the selected row")
	}
}
