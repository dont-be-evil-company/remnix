package search

import (
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/history"
)

func TestRankFuzzyPrefersPrefix(t *testing.T) {
	entries := []history.Entry{
		{ID: "1", Command: "git status", StartTS: time.Unix(2, 0)},
		{ID: "2", Command: "go test ./...", StartTS: time.Unix(3, 0)},
		{ID: "3", Command: "cat git", StartTS: time.Unix(1, 0)},
	}
	got := Rank("git", entries, false)
	if len(got) < 2 {
		t.Fatalf("got %d", len(got))
	}
	if got[0].Entry.Command != "git status" {
		t.Fatalf("want git status first, got %q score %d vs %q %d", got[0].Entry.Command, got[0].Score, got[1].Entry.Command, got[1].Score)
	}
}

func TestRankPrefersSubstringOverScatteredLetters(t *testing.T) {
	entries := []history.Entry{
		{Command: "jj ci setup/install-fonts.sh configurations/fonts/departure-mono-nerd-font/", StartTS: time.Unix(100, 0)},
		{Command: `# Default to "FiraCode Nerd Font Mono" if no font name is provided\`, StartTS: time.Unix(90, 0)},
		{Command: "docker run --rm alpine du -h /", StartTS: time.Unix(80, 0)},
		{Command: "syncsh daemon uninstall", StartTS: time.Unix(50, 0)},
		{Command: "syncsh daemon install", StartTS: time.Unix(51, 0)},
		{Command: "syncsh daemon status", StartTS: time.Unix(60, 0)},
	}
	got := Rank("daemon", entries, false)
	if len(got) < 3 {
		t.Fatalf("got %d", len(got))
	}
	for i, want := range []string{
		"syncsh daemon status",
		"syncsh daemon install",
		"syncsh daemon uninstall",
	} {
		if got[i].Entry.Command != want {
			t.Fatalf("rank %d: want %q, got %q (score %d)", i, want, got[i].Entry.Command, got[i].Score)
		}
	}
	for i := 3; i < len(got); i++ {
		if got[i].Score >= got[0].Score {
			t.Fatalf("loose match %q should score below substring, got %d vs %d", got[i].Entry.Command, got[i].Score, got[0].Score)
		}
	}
}

func TestRankMultiTermRequiresAll(t *testing.T) {
	entries := []history.Entry{
		{Command: "syncsh daemon status", StartTS: time.Unix(2, 0)},
		{Command: "systemctl status nginx", StartTS: time.Unix(3, 0)},
	}
	got := Rank("syncsh daemon", entries, false)
	if len(got) != 1 || got[0].Entry.Command != "syncsh daemon status" {
		t.Fatalf("got %+v", got)
	}
}

func TestRankExactSubstring(t *testing.T) {
	entries := []history.Entry{
		{Command: "echo hello"},
		{Command: "ls"},
	}
	got := Rank("hello", entries, true)
	if len(got) != 1 || got[0].Entry.Command != "echo hello" {
		t.Fatalf("got %+v", got)
	}
}

func TestRankEmptyQueryNewestFirst(t *testing.T) {
	entries := []history.Entry{
		{Command: "old", StartTS: time.Unix(1, 0)},
		{Command: "new", StartTS: time.Unix(9, 0)},
	}
	got := Rank("", entries, false)
	if len(got) != 2 || got[0].Entry.Command != "new" {
		t.Fatalf("got %+v", got)
	}
}

func TestMinSpanConsecutive(t *testing.T) {
	span, ok := minSpan([]rune("daemon"), []rune("syncsh daemon uninstall"))
	if !ok || span != 6 {
		t.Fatalf("span=%d ok=%v", span, ok)
	}
	loose, ok := minSpan([]rune("daemon"), []rune("departure-mono-nerd-font"))
	if !ok {
		t.Fatal("expected subsequence match")
	}
	if loose <= span {
		t.Fatalf("loose span %d should be wider than consecutive %d", loose, span)
	}
}
