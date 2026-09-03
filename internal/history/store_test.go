package history

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/db"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return NewStore(d)
}

func TestInsertGetComplete(t *testing.T) {
	s := testStore(t)
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	e := Entry{
		ID:       "h1",
		Command:  "ls -la",
		StartTS:  start,
		Cwd:      "/tmp",
		DeviceID: "dev",
		Shell:    "zsh",
	}
	ok, err := s.Insert(e)
	if err != nil || !ok {
		t.Fatalf("insert: ok=%v err=%v", ok, err)
	}
	got, found, err := s.Get("h1")
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if got.Command != "ls -la" || got.Cwd != "/tmp" {
		t.Fatalf("got %+v", got)
	}
	end := start.Add(1500 * time.Millisecond)
	if err := s.Complete("h1", end, 0); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.Get("h1")
	if got.ExitStatus == nil || *got.ExitStatus != 0 {
		t.Fatalf("exit %+v", got.ExitStatus)
	}
	if got.DurationMs == nil || *got.DurationMs != 1500 {
		t.Fatalf("duration %+v", got.DurationMs)
	}
}

func TestInsertIdempotent(t *testing.T) {
	s := testStore(t)
	e := Entry{ID: "h1", Command: "echo hi", StartTS: time.UnixMilli(1).UTC(), Cwd: "/x", DeviceID: "d"}
	ok, err := s.Insert(e)
	if err != nil || !ok {
		t.Fatal(err)
	}
	e.ID = "h2"
	ok, err = s.Insert(e)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected duplicate to be ignored")
	}
}

func TestListFilters(t *testing.T) {
	s := testStore(t)
	now := time.UnixMilli(1000).UTC()
	_, _ = s.Insert(Entry{ID: "1", Command: "git status", StartTS: now, Cwd: "/a", DeviceID: "d", Shell: "zsh", Hostname: "host1"})
	exit1 := 1
	_, _ = s.Insert(Entry{ID: "2", Command: "make test", StartTS: now.Add(time.Second), Cwd: "/b", DeviceID: "d", Shell: "bash", Hostname: "host2", ExitStatus: &exit1})
	exit := 1
	got, err := s.List(Filter{Exit: &exit, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "make test" {
		t.Fatalf("got %+v", got)
	}
	got, _ = s.List(Filter{Query: "git", Limit: 10})
	if len(got) != 1 {
		t.Fatalf("query got %+v", got)
	}
	got, err = s.List(Filter{Cwd: "/a", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "git status" || got[0].Cwd != "/a" {
		t.Fatalf("cwd %+v", got)
	}
	got, err = s.List(Filter{Cwd: "/missing", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("missing cwd %+v", got)
	}
	var internN int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM intern_cwd`).Scan(&internN); err != nil {
		t.Fatal(err)
	}
	if internN != 2 {
		t.Fatalf("filter must not intern unknown cwd, got %d", internN)
	}
}

func TestListUniqueKeepsNewestPerCommand(t *testing.T) {
	s := testStore(t)
	_, _ = s.Insert(Entry{ID: "1", Command: "git pull", StartTS: time.UnixMilli(1).UTC(), DeviceID: "d", Cwd: "/a"})
	_, _ = s.Insert(Entry{ID: "2", Command: "git status", StartTS: time.UnixMilli(2).UTC(), DeviceID: "d", Cwd: "/a"})
	_, _ = s.Insert(Entry{ID: "3", Command: "git pull", StartTS: time.UnixMilli(3).UTC(), DeviceID: "d", Cwd: "/b"})
	got, err := s.List(Filter{Unique: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d %+v", len(got), got)
	}
	if got[0].Command != "git pull" || got[0].ID != "3" {
		t.Fatalf("want newest git pull first: %+v", got[0])
	}
	if got[1].Command != "git status" {
		t.Fatalf("got %+v", got[1])
	}
	cwd, err := s.List(Filter{Unique: true, Cwd: "/a", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(cwd) != 2 {
		t.Fatalf("cwd unique %d %+v", len(cwd), cwd)
	}
	all, err := s.List(Filter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("non-unique %d", len(all))
	}
}

func TestTombstoneHidesFromList(t *testing.T) {
	s := testStore(t)
	_, _ = s.Insert(Entry{ID: "1", Command: "rm -rf /", StartTS: time.UnixMilli(1).UTC(), DeviceID: "d"})
	if err := s.Tombstone("1"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.List(Filter{Limit: 10})
	if len(got) != 0 {
		t.Fatalf("expected hidden, got %+v", got)
	}
}

func TestListByCommand(t *testing.T) {
	s := testStore(t)
	_, _ = s.Insert(Entry{ID: "1", Command: "secret", StartTS: time.UnixMilli(1).UTC(), DeviceID: "d"})
	_, _ = s.Insert(Entry{ID: "2", Command: "secret", StartTS: time.UnixMilli(2).UTC(), DeviceID: "d"})
	_, _ = s.Insert(Entry{ID: "3", Command: "ls", StartTS: time.UnixMilli(3).UTC(), DeviceID: "d"})
	got, err := s.ListByCommand("secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "2" || got[1].ID != "1" {
		t.Fatalf("got %+v", got)
	}
	if err := s.Tombstone("2"); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListByCommand("secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "1" {
		t.Fatalf("after tombstone %+v", got)
	}
}

func TestCommandSummaries(t *testing.T) {
	s := testStore(t)
	ok := 0
	fail := 1
	durNew := int64(20)
	durOld := int64(5)
	base := time.UnixMilli(1_000).UTC()
	insert := func(id, cmd string, ts time.Time, cwd, host string, exit *int, dur *int64) {
		t.Helper()
		e := Entry{
			ID: id, Command: cmd, StartTS: ts, Cwd: cwd, Hostname: host,
			DeviceID: "d", ExitStatus: exit, DurationMs: dur,
		}
		if _, err := s.Insert(e); err != nil {
			t.Fatal(err)
		}
	}
	insert("1", "git status", base, "/old", "host-a", &ok, &durOld)
	insert("2", "git status", base.Add(time.Second), "/new", "host-b", &fail, &durNew)
	insert("3", "git status", base.Add(2*time.Second), "/new", "host-b", &ok, &durNew)
	insert("4", "ls", base.Add(3*time.Second), "/tmp", "host-a", &ok, &durOld)
	insert("5", "make test", base.Add(4*time.Second), "/src", "host-a", &fail, &durNew)
	_, _ = s.Insert(Entry{ID: "6", Command: "gone", StartTS: base, DeviceID: "d"})
	if err := s.Tombstone("6"); err != nil {
		t.Fatal(err)
	}

	got, err := s.CommandSummaries(SummaryFilter{Sort: SortRecent, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("recent len %d %+v", len(got), got)
	}
	if got[0].Command != "make test" || got[1].Command != "ls" || got[2].Command != "git status" {
		t.Fatalf("recent order %+v", commandsOf(got))
	}

	gs := got[2]
	if gs.Runs != 3 || gs.Success != 2 || gs.Failed != 1 {
		t.Fatalf("git status counts %+v", gs)
	}
	if gs.FirstTS != base || gs.LastTS != base.Add(2*time.Second) {
		t.Fatalf("git status times first=%s last=%s", gs.FirstTS, gs.LastTS)
	}
	if gs.LastCwd != "/new" || gs.LastHost != "host-b" || gs.LastExit == nil || *gs.LastExit != 0 {
		t.Fatalf("last row %+v", gs)
	}
	if gs.LastDurationMs == nil || *gs.LastDurationMs != durNew {
		t.Fatalf("last duration %+v", gs.LastDurationMs)
	}

	top, err := s.CommandSummaries(SummaryFilter{Sort: SortTop, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 3 || top[0].Command != "git status" || top[0].Runs != 3 {
		t.Fatalf("top %+v", top)
	}

	failed, err := s.CommandSummaries(SummaryFilter{Sort: SortFailed, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 2 || failed[0].Command != "make test" || failed[1].Command != "git status" {
		t.Fatalf("failed %+v", commandsOf(failed))
	}

	q, err := s.CommandSummaries(SummaryFilter{Query: "git", Sort: SortRecent, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(q) != 1 || q[0].Command != "git status" {
		t.Fatalf("query %+v", q)
	}

	limited, err := s.CommandSummaries(SummaryFilter{Sort: SortTop, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0].Command != "git status" {
		t.Fatalf("limit %+v", limited)
	}
}

func commandsOf(in []CommandSummary) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = s.Command
	}
	return out
}

func TestSuggestPrefix(t *testing.T) {
	s := testStore(t)
	_, _ = s.Insert(Entry{ID: "1", Command: "git status", StartTS: time.UnixMilli(1).UTC(), DeviceID: "d"})
	_, _ = s.Insert(Entry{ID: "2", Command: "git push -u origin HEAD", StartTS: time.UnixMilli(2).UTC(), DeviceID: "d"})
	_, _ = s.Insert(Entry{ID: "3", Command: "jj push", StartTS: time.UnixMilli(3).UTC(), DeviceID: "d"})
	got, err := s.SuggestPrefix("git p")
	if err != nil || got != "git push -u origin HEAD" {
		t.Fatalf("got %q err=%v", got, err)
	}
	got, err = s.SuggestPrefix("git")
	if err != nil || got != "git push -u origin HEAD" {
		t.Fatalf("most recent git*: %q err=%v", got, err)
	}
	got, err = s.SuggestPrefix("missing")
	if err != nil || got != "" {
		t.Fatalf("empty: %q err=%v", got, err)
	}
	_, _ = s.Insert(Entry{ID: "4", Command: "100% done", StartTS: time.UnixMilli(4).UTC(), DeviceID: "d"})
	got, err = s.SuggestPrefix("100%")
	if err != nil || got != "100% done" {
		t.Fatalf("like escape: %q err=%v", got, err)
	}
}

func TestInternReusesCwd(t *testing.T) {
	s := testStore(t)
	_, _ = s.Insert(Entry{ID: "1", Command: "ls", StartTS: time.UnixMilli(1).UTC(), DeviceID: "d", Cwd: "/proj"})
	_, _ = s.Insert(Entry{ID: "2", Command: "pwd", StartTS: time.UnixMilli(2).UTC(), DeviceID: "d", Cwd: "/proj"})
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM intern_cwd`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cwd intern rows %d", n)
	}
	got, found, err := s.Get("2")
	if err != nil || !found || got.Cwd != "/proj" {
		t.Fatalf("cwd %+v found=%v err=%v", got, found, err)
	}
}

func TestInternReusesSessionHostShell(t *testing.T) {
	s := testStore(t)
	_, _ = s.Insert(Entry{ID: "1", Command: "ls", StartTS: time.UnixMilli(1).UTC(), DeviceID: "d", SessionID: "s1", Hostname: "box", Shell: "zsh"})
	_, _ = s.Insert(Entry{ID: "2", Command: "pwd", StartTS: time.UnixMilli(2).UTC(), DeviceID: "d", SessionID: "s1", Hostname: "box", Shell: "zsh"})
	assertCount := func(table string) {
		t.Helper()
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%s intern rows %d", table, n)
		}
	}
	assertCount("intern_session")
	assertCount("intern_hostname")
	assertCount("intern_shell")
	got, err := s.List(Filter{Session: "s1", Host: "box", Shell: "zsh", Limit: 10})
	if err != nil || len(got) != 2 {
		t.Fatalf("intern filters: n=%d err=%v", len(got), err)
	}
}

func TestCommandHashDedup(t *testing.T) {
	s := testStore(t)
	a := Entry{ID: "1", Command: "echo hi", StartTS: time.UnixMilli(1).UTC(), Cwd: "/x", DeviceID: "d"}
	ok, err := s.Insert(a)
	if err != nil || !ok {
		t.Fatal(err)
	}
	a.ID = "2"
	ok, err = s.Insert(a)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected hash unique index to ignore duplicate")
	}
}
