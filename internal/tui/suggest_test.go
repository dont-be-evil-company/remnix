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
	if m.cursor != 0 {
		t.Fatalf("backspace should keep the typed row selected, cursor=%d", m.cursor)
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

func TestSuggestMenuDescrsArriveLater(t *testing.T) {
	ch := make(chan SuggestItems)
	m := newSuggestModel(SuggestMenuOptions{Prefix: "gcloud ", ItemsCh: ch})
	got, _ := m.Update(suggestItemsMsg{items: []suggestItem{{cmd: "gcloud auth"}}})
	m = got.(suggestModel)
	if m.loading || m.items[0].descr != "" {
		t.Fatalf("loading=%v descr=%q", m.loading, m.items[0].descr)
	}
	got, _ = m.Update(suggestItemsMsg{items: []suggestItem{{cmd: "gcloud auth", descr: "Manage oauth2 credentials"}}})
	m = got.(suggestModel)
	if m.items[0].descr != "Manage oauth2 credentials" {
		t.Fatalf("descr %q", m.items[0].descr)
	}
	if m.cursor != 1 {
		t.Fatalf("cursor %d", m.cursor)
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

func TestSuggestCacheBestLongestPrefix(t *testing.T) {
	c := NewSuggestCache()
	c.Store("remnix", []string{"remnix changelog", "remnix daemon", "remnix database"}, nil)
	c.Store("remnix daemon ", []string{"remnix daemon compact", "remnix daemon install"}, nil)
	key, items, ok := c.best("remnix daemon compact")
	if !ok || key != "remnix daemon " || len(items) != 2 {
		t.Fatalf("child key=%q items=%+v ok=%v", key, items, ok)
	}
	key, items, ok = c.best("remnix daemon")
	if !ok || key != "remnix" || len(items) != 3 {
		t.Fatalf("parent after dropping trailing space key=%q items=%+v ok=%v", key, items, ok)
	}
	if !c.Has("remnix daemon") || !c.Has("remnix") {
		t.Fatal("Has should treat trailing space as the same level")
	}
}

func TestSuggestMenuBackspaceRestoresCachedParent(t *testing.T) {
	cache := NewSuggestCache()
	cache.Store("remnix", []string{"remnix changelog", "remnix daemon", "remnix database"}, nil)
	cache.Store("remnix daemon ", []string{"remnix daemon compact", "remnix daemon install"}, nil)
	m := newSuggestModel(SuggestMenuOptions{
		Prefix: "remnix daemon ",
		Items:  []string{"remnix daemon compact", "remnix daemon install"},
		Cache:  cache,
	})
	if m.cursor != 1 || len(m.items) != 2 || m.items[0].cmd != "remnix daemon compact" {
		t.Fatalf("start cursor=%d items=%+v", m.cursor, m.items)
	}
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = got.(suggestModel)
	if m.prefix != "remnix daemon" || m.cursor != 0 {
		t.Fatalf("after space drop prefix=%q cursor=%d", m.prefix, m.cursor)
	}
	for i := 0; i < len("daemon"); i++ {
		got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = got.(suggestModel)
	}
	if m.prefix != "remnix " {
		t.Fatalf("prefix %q", m.prefix)
	}
	if m.cursor != 0 {
		t.Fatalf("cursor %d want typed row", m.cursor)
	}
	if len(m.items) != 3 {
		t.Fatalf("parent items %+v", m.items)
	}
	if m.ContinueSelected() || m.quitting {
		t.Fatal("restoring parent from cache must stay in the overlay")
	}
}

func TestSuggestMenuCtrlSpaceCachedChildDrillsInPlace(t *testing.T) {
	cache := NewSuggestCache()
	cache.Store("remnix", []string{"remnix daemon", "remnix database"}, nil)
	cache.Store("remnix daemon ", []string{"remnix daemon compact", "remnix daemon install"}, nil)
	m := newSuggestModel(SuggestMenuOptions{
		Prefix: "remnix",
		Items:  []string{"remnix daemon", "remnix database"},
		Cache:  cache,
	})
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	m = got.(suggestModel)
	if m.quitting || m.ContinueSelected() {
		t.Fatal("cached child should drill in-place")
	}
	if m.prefix != "remnix daemon " {
		t.Fatalf("prefix %q", m.prefix)
	}
	if m.cursor != 1 || len(m.items) != 2 || m.items[0].cmd != "remnix daemon compact" {
		t.Fatalf("cursor=%d items=%+v", m.cursor, m.items)
	}
}

func TestSuggestMenuCtrlSpaceTypedRowCachedStays(t *testing.T) {
	m := newSuggestModel(SuggestMenuOptions{
		Prefix: "remnix",
		Items:  []string{"remnix daemon", "remnix database"},
	})
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = got.(suggestModel)
	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	m = got.(suggestModel)
	if m.quitting || m.ContinueSelected() {
		t.Fatal("typed row with cached prefix should stay")
	}
	if m.prefix != "remnix" || m.cursor != 0 {
		t.Fatalf("prefix=%q cursor=%d", m.prefix, m.cursor)
	}
}

func TestSuggestMenuCtrlSpaceUncachedChildContinues(t *testing.T) {
	m := newSuggestModel(SuggestMenuOptions{
		Prefix: "remnix",
		Items:  []string{"remnix daemon", "remnix database"},
	})
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl})
	gm := got.(suggestModel)
	cmd, ok := gm.SelectedCommand()
	if !ok || cmd != "remnix daemon" {
		t.Fatalf("got %q ok=%v", cmd, ok)
	}
	if !gm.ContinueSelected() || gm.RunSelected() {
		t.Fatal("uncached child should continue so the completer can run")
	}
}

func TestSuggestMenuBackspaceCtrlSpaceUsesTypedPrefix(t *testing.T) {
	m := newSuggestModel(SuggestMenuOptions{
		Prefix: "gcloud storage",
		Items:  []string{"gcloud storage buckets"},
	})
	got, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m = got.(suggestModel)
	if m.cursor != 0 || m.prefix != "gcloud storag" {
		t.Fatalf("prefix=%q cursor=%d", m.prefix, m.cursor)
	}
	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	gm := got.(suggestModel)
	cmd, ok := gm.SelectedCommand()
	if !ok || cmd != "gcloud storag" || gm.ContinueSelected() {
		t.Fatalf("typed after backspace: %q ok=%v cont=%v", cmd, ok, gm.ContinueSelected())
	}
}
