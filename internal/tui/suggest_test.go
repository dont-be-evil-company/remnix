package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSuggestMenuEnterInserts(t *testing.T) {
	m := newSuggestModel(SuggestMenuOptions{Prefix: "git s", Items: []string{"git status", "git stash"}})
	if m.cursor != 1 {
		t.Fatalf("cursor %d", m.cursor)
	}
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	gm := got.(suggestModel)
	cmd, ok := gm.SelectedCommand()
	if !ok || cmd != "git status" {
		t.Fatalf("got %q ok=%v", cmd, ok)
	}
	if gm.RunSelected() {
		t.Fatal("enter should insert for edit, not run")
	}
	v := newSuggestModel(SuggestMenuOptions{Prefix: "git", Items: []string{"git status"}}).View()
	if strings.Contains(v.Content, "ctrl+o") || strings.Contains(v.Content, " run") {
		t.Fatalf("lsp menu must not advertise run/ctrl+o: %q", v.Content)
	}
}

func TestSuggestMenuEscCancels(t *testing.T) {
	m := newSuggestModel(SuggestMenuOptions{Prefix: "git", Items: []string{"git status"}})
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	gm := got.(suggestModel)
	if _, ok := gm.SelectedCommand(); ok {
		t.Fatal("esc should not print")
	}
}

func TestSuggestMenuTypedRowDoesNotRun(t *testing.T) {
	m := newSuggestModel(SuggestMenuOptions{Prefix: "git", Items: []string{"git status"}})
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	gm := got.(suggestModel)
	got, _ = gm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	gm = got.(suggestModel)
	cmd, ok := gm.SelectedCommand()
	if !ok || cmd != "git" {
		t.Fatalf("typed: %q ok=%v", cmd, ok)
	}
	if gm.RunSelected() {
		t.Fatal("enter on typed row should insert, not run")
	}
}

func TestSuggestMenuTypingNarrowsItems(t *testing.T) {
	m := newSuggestModel(SuggestMenuOptions{
		Prefix: "gcloud",
		Items:  []string{"gcloud compute", "gcloud storage", "gcloud auth login"},
	})
	got, _ := m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	m = got.(suggestModel)
	got, _ = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = got.(suggestModel)
	if m.prefix != "gcloud s" {
		t.Fatalf("prefix %q", m.prefix)
	}
	if len(m.items) != 1 || m.items[0].cmd != "gcloud storage" {
		t.Fatalf("items %+v", m.items)
	}
	if m.cursor != 1 {
		t.Fatalf("cursor %d", m.cursor)
	}
	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = got.(suggestModel)
	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = got.(suggestModel)
	if m.prefix != "gcloud" || len(m.items) != 3 {
		t.Fatalf("after backspace prefix=%q items=%+v", m.prefix, m.items)
	}
}

func TestSuggestMenuCtrlSpaceContinues(t *testing.T) {
	m := newSuggestModel(SuggestMenuOptions{
		Prefix: "mise",
		Items:  []string{"mise install", "mise use"},
	})
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	gm := got.(suggestModel)
	cmd, ok := gm.SelectedCommand()
	if !ok || cmd != "mise install" {
		t.Fatalf("got %q ok=%v", cmd, ok)
	}
	if gm.RunSelected() || !gm.ContinueSelected() {
		t.Fatal("ctrl+space should insert into typed and continue, not run")
	}
	v := newSuggestModel(SuggestMenuOptions{Prefix: "mise", Items: []string{"mise install"}}).View()
	if !strings.Contains(v.Content, "ctrl+space") {
		t.Fatalf("help should mention ctrl+space: %q", v.Content)
	}
}

func TestSuggestMenuTypingNoMatchKeepsTypedRow(t *testing.T) {
	m := newSuggestModel(SuggestMenuOptions{Prefix: "git", Items: []string{"git status"}})
	got, _ := m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	m = got.(suggestModel)
	if m.prefix != "gitz" || len(m.items) != 0 || m.cursor != 0 {
		t.Fatalf("prefix=%q items=%+v cursor=%d", m.prefix, m.items, m.cursor)
	}
	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	gm := got.(suggestModel)
	cmd, ok := gm.SelectedCommand()
	if !ok || cmd != "gitz" || gm.RunSelected() {
		t.Fatalf("typed no-match: %q ok=%v run=%v", cmd, ok, gm.RunSelected())
	}
}

func TestSuggestViewUsesItemIcon(t *testing.T) {
	m := newSuggestModel(SuggestMenuOptions{
		Prefix:   "gcloud",
		Items:    []string{"gcloud compute"},
		ItemIcon: "+",
	})
	v := m.View()
	if !strings.Contains(v.Content, "+ gcloud compute") {
		t.Fatalf("content: %q", v.Content)
	}
	if strings.Contains(v.Content, "* gcloud compute") {
		t.Fatal("completion rows must not use the history icon")
	}
}

func TestSuggestViewShowsDescriptions(t *testing.T) {
	m := newSuggestModel(SuggestMenuOptions{
		Prefix: "mise",
		Items:  []string{"mise install", "mise use"},
		Descrs: []string{"Install a tool version", "Set the active runtime"},
	})
	v := m.View()
	if !strings.Contains(v.Content, "mise install") || !strings.Contains(v.Content, "Install a tool version") {
		t.Fatalf("missing label/description: %q", v.Content)
	}
	if !strings.Contains(v.Content, "Set the active runtime") {
		t.Fatalf("missing second description: %q", v.Content)
	}
}

func TestSuggestViewInlineWhenOverlay(t *testing.T) {
	m := newSuggestModel(SuggestMenuOptions{Prefix: "x", Items: []string{"xy"}})
	m.EnableOverlay(OverlayState{TermCols: 40, TermRows: 8, RectH: 8})
	v := m.View()
	if v.AltScreen {
		t.Fatal("overlay is inline on the main buffer, like Atuin Viewport::Fixed")
	}
	if !strings.Contains(v.Content, "xy") {
		t.Fatalf("content: %q", v.Content)
	}
}

func TestSearchViewInlineWhenOverlay(t *testing.T) {
	m := New(nil, Options{})
	m.EnableOverlay(OverlayState{TermCols: 40, TermRows: 8, RectH: 8})
	v := m.View()
	if v.AltScreen {
		t.Fatal("overlay is inline on the main buffer, like Atuin Viewport::Fixed")
	}
}

func TestSearchViewAltScreenWhenFullscreen(t *testing.T) {
	m := New(nil, Options{})
	v := m.View()
	if !v.AltScreen {
		t.Fatal("fullscreen search (no overlay) uses the alt-screen")
	}
}

func TestSuggestMenuShowsSpinnerWhileLoading(t *testing.T) {
	ch := make(chan SuggestItems)
	m := newSuggestModel(SuggestMenuOptions{Prefix: "gcloud storage", ItemsCh: ch})
	if !m.loading || m.cursor != 0 {
		t.Fatalf("loading=%v cursor=%d", m.loading, m.cursor)
	}
	v := m.View()
	if !strings.Contains(v.Content, "loading completions") {
		t.Fatalf("missing loading copy: %q", v.Content)
	}
	if !strings.Contains(v.Content, suggestSpinFrames[0]) {
		t.Fatalf("missing spinner: %q", v.Content)
	}
	got, cmd := m.Update(suggestSpinMsg{})
	m = got.(suggestModel)
	if m.spinFrame != 1 || cmd == nil {
		t.Fatalf("frame=%d cmd=%v", m.spinFrame, cmd)
	}
	got, _ = m.Update(suggestItemsMsg{items: []suggestItem{{cmd: "gcloud storage buckets", descr: "manage buckets"}}})
	m = got.(suggestModel)
	if m.loading {
		t.Fatal("items should end loading")
	}
	if len(m.items) != 1 || m.items[0].cmd != "gcloud storage buckets" {
		t.Fatalf("items %+v", m.items)
	}
	v = m.View()
	if strings.Contains(v.Content, "loading completions") {
		t.Fatalf("spinner lingered: %q", v.Content)
	}
	if !strings.Contains(v.Content, "gcloud storage buckets") || !strings.Contains(v.Content, "manage buckets") {
		t.Fatalf("missing loaded row: %q", v.Content)
	}
}

func TestSuggestMenuAbortWhileLoading(t *testing.T) {
	ch := make(chan SuggestItems)
	m := newSuggestModel(SuggestMenuOptions{Prefix: "gcloud", ItemsCh: ch})
	got, _ := m.Update(suggestItemsMsg{abort: true})
	gm := got.(suggestModel)
	if !gm.quitting {
		t.Fatal("abort should quit")
	}
	if _, ok := gm.SelectedCommand(); ok {
		t.Fatal("abort should not insert")
	}
}

func TestSuggestMenuCtrlSpaceIgnoredWhileLoading(t *testing.T) {
	ch := make(chan SuggestItems)
	m := newSuggestModel(SuggestMenuOptions{Prefix: "gcloud", ItemsCh: ch})
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	gm := got.(suggestModel)
	if gm.quitting || gm.ContinueSelected() || !gm.loading {
		t.Fatal("ctrl+space during load should leave the spinner running")
	}
}
