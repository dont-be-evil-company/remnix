package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestSuggestMenuEnterRunsHistory(t *testing.T) {
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
	if !gm.RunSelected() {
		t.Fatal("enter on history should run")
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
