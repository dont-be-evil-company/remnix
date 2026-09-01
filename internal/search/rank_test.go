package search

import (
	"fmt"
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

func TestRankCwdAndFrequency(t *testing.T) {
	now := time.Unix(1000, 0)
	entries := []history.Entry{
		{Command: "make test", StartTS: time.Unix(10, 0), Cwd: "/old"},
		{Command: "make test", StartTS: time.Unix(20, 0), Cwd: "/old"},
		{Command: "make clean", StartTS: time.Unix(900, 0), Cwd: "/proj"},
	}
	got := RankWith("make", entries, false, Context{Now: now, Cwd: "/proj"})
	if got[0].Entry.Command != "make clean" {
		t.Fatalf("cwd should lift recent make clean, got %q", got[0].Entry.Command)
	}
}

func TestRankAncientFrequentDoesNotBeatRecentPrefix(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	entries := []history.Entry{
		{Command: "ls", StartTS: time.Unix(1, 0)},
		{Command: "ls", StartTS: time.Unix(2, 0)},
		{Command: "ls", StartTS: time.Unix(3, 0)},
		{Command: "ls", StartTS: time.Unix(4, 0)},
		{Command: "lsync status", StartTS: time.Unix(999_000, 0)},
	}
	got := RankWith("ls", entries, false, Context{Now: now})
	if got[0].Entry.Command != "lsync status" && got[0].Entry.Command != "ls" {
		t.Fatalf("got %q", got[0].Entry.Command)
	}
	if got[0].Entry.Command != "ls" && got[0].Parts.Match < classPrefix*classWeight-100 {
		t.Fatalf("prefix match should dominate: %+v", got[0])
	}
}

func TestMinSpan(t *testing.T) {
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

func TestBestSuggestionSkipsIdentical(t *testing.T) {
	entries := []history.Entry{
		{Command: "git", StartTS: time.Unix(3, 0)},
		{Command: "git status", StartTS: time.Unix(2, 0)},
	}
	got := BestSuggestion("git", entries, Context{})
	if got != "git status" {
		t.Fatalf("got %q", got)
	}
	if BestSuggestion("missing", entries, Context{}) != "" {
		t.Fatal("expected no suggestion")
	}
}

func TestSuggestionsUniqueAndLimit(t *testing.T) {
	entries := []history.Entry{
		{Command: "git status", StartTS: time.Unix(5, 0)},
		{Command: "git status", StartTS: time.Unix(4, 0)},
		{Command: "git stash", StartTS: time.Unix(3, 0)},
		{Command: "git", StartTS: time.Unix(2, 0)},
		{Command: "git switch", StartTS: time.Unix(1, 0)},
	}
	got := Suggestions("git", entries, Context{}, 2)
	if len(got) != 2 {
		t.Fatalf("limit: %v", got)
	}
	seen := map[string]struct{}{}
	for _, s := range got {
		if s == "git" {
			t.Fatal("should skip exact prefix")
		}
		if _, ok := seen[s]; ok {
			t.Fatalf("duplicate %q", s)
		}
		seen[s] = struct{}{}
	}
	all := Suggestions("git", entries, Context{}, 10)
	if len(all) != 3 {
		t.Fatalf("unique all: %v", all)
	}
}

func BenchmarkRank(b *testing.B) {
	entries := make([]history.Entry, 8000)
	now := time.Unix(1_000_000, 0)
	for i := range entries {
		entries[i] = history.Entry{
			Command:  fmt.Sprintf("cmd-%d git status extra %d", i%50, i),
			StartTS:  now.Add(-time.Duration(i) * time.Minute),
			Cwd:      fmt.Sprintf("/p/%d", i%20),
			DeviceID: "d1",
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = RankWith("git st", entries, false, Context{Now: now, Cwd: "/p/1", DeviceID: "d1"})
	}
}
