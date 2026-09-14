package history

import (
	"regexp"
	"testing"
	"time"
)

func mustRE(t *testing.T, pat string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(pat)
	if err != nil {
		t.Fatal(err)
	}
	return re
}

func TestCommandSummariesAdvancedFilters(t *testing.T) {
	s := testStore(t)
	ok := 0
	fail := 1
	base := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	insert := func(id, cmd, cwd, host, shell, sess, dev string, ts time.Time, exit *int) {
		t.Helper()
		e := Entry{
			ID: id, Command: cmd, StartTS: ts, Cwd: cwd, Hostname: host,
			Shell: shell, SessionID: sess, DeviceID: dev, ExitStatus: exit,
		}
		if _, err := s.Insert(e); err != nil {
			t.Fatal(err)
		}
	}
	insert("1", "git status", "/repo", "dev", "zsh", "s1", "d1", base, &ok)
	insert("2", "git status", "/old", "laptop", "zsh", "s2", "d1", base.Add(time.Hour), &fail)
	insert("3", "ls", "/tmp", "dev", "bash", "s1", "d2", base.Add(2*time.Hour), &ok)
	insert("4", "make test", "/src", "dev", "zsh", "s3", "d1", base.Add(3*time.Hour), &fail)

	got, err := s.CommandSummariesAdvanced(AdvancedFilter{
		CwdRE: mustRE(t, "/repo|/old"),
		Sort:  SortRecent,
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "git status" || got[0].Runs != 2 {
		t.Fatalf("cwd regex %+v", got)
	}
	if got[0].LastCwd != "/old" || got[0].Success != 1 || got[0].Failed != 1 {
		t.Fatalf("matching-run aggregates %+v", got[0])
	}

	got, err = s.CommandSummariesAdvanced(AdvancedFilter{
		HostRE: mustRE(t, "^dev$"),
		Sort:   SortRecent,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("host regex len %d %+v", len(got), commandsOf(got))
	}
	byCmd := map[string]CommandSummary{}
	for _, cs := range got {
		byCmd[cs.Command] = cs
	}
	if byCmd["git status"].Runs != 1 || byCmd["git status"].LastCwd != "/repo" {
		t.Fatalf("host-filtered git status %+v", byCmd["git status"])
	}

	got, err = s.CommandSummariesAdvanced(AdvancedFilter{
		CommandRE: mustRE(t, `^git `),
		CwdRE:     mustRE(t, `/repo`),
		Sort:      SortRecent,
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "git status" || got[0].Runs != 1 {
		t.Fatalf("command+cwd AND %+v", got)
	}

	got, err = s.CommandSummariesAdvanced(AdvancedFilter{
		CwdRE: mustRE(t, `/nope`),
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("impossible intern match %+v", got)
	}

	since := base.Add(90 * time.Minute)
	got, err = s.CommandSummariesAdvanced(AdvancedFilter{
		Since: &since,
		Sort:  SortRecent,
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Command != "make test" || got[1].Command != "ls" {
		t.Fatalf("since %+v", commandsOf(got))
	}

	got, err = s.CommandSummariesAdvanced(AdvancedFilter{ExitFailed: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("failed %d %+v", len(got), commandsOf(got))
	}

	zero := 0
	got, err = s.CommandSummariesAdvanced(AdvancedFilter{Exit: &zero, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("exit 0 %d %+v", len(got), commandsOf(got))
	}

	got, err = s.CommandSummariesAdvanced(AdvancedFilter{
		ExitRE: mustRE(t, `^1$`),
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("exit regex %d %+v", len(got), commandsOf(got))
	}

	got, err = s.CommandSummariesAdvanced(AdvancedFilter{
		ShellRE:   mustRE(t, `zsh`),
		SessionRE: mustRE(t, `^s1$`),
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "git status" || got[0].Runs != 1 {
		t.Fatalf("shell+session %+v", got)
	}

	got, err = s.CommandSummariesAdvanced(AdvancedFilter{
		DeviceRE: mustRE(t, `d2`),
		Limit:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "ls" {
		t.Fatalf("device %+v", got)
	}
}

func TestListByCommandMatching(t *testing.T) {
	s := testStore(t)
	ok := 0
	fail := 1
	base := time.UnixMilli(1000).UTC()
	_, _ = s.Insert(Entry{ID: "1", Command: "git status", StartTS: base, Cwd: "/repo", Hostname: "dev", DeviceID: "d", ExitStatus: &ok})
	_, _ = s.Insert(Entry{ID: "2", Command: "git status", StartTS: base.Add(time.Second), Cwd: "/old", Hostname: "laptop", DeviceID: "d", ExitStatus: &fail})
	_, _ = s.Insert(Entry{ID: "3", Command: "ls", StartTS: base.Add(2 * time.Second), Cwd: "/repo", Hostname: "dev", DeviceID: "d", ExitStatus: &ok})

	got, err := s.ListByCommandMatching("git status", AdvancedFilter{CwdRE: mustRE(t, `/old`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "2" {
		t.Fatalf("got %+v", got)
	}

	got, err = s.ListByCommandMatching("git status", AdvancedFilter{CommandRE: mustRE(t, `^ls$`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("command regex should exclude %+v", got)
	}

	got, err = s.ListByCommandMatching("git status", AdvancedFilter{})
	if err != nil || len(got) != 2 {
		t.Fatalf("empty filter n=%d err=%v", len(got), err)
	}
}

func TestAdvancedFilterMatch(t *testing.T) {
	ok := 0
	fail := 1
	e := Entry{Command: "git status", Cwd: "/repo", Hostname: "dev", Shell: "zsh", SessionID: "s1", DeviceID: "d1", ExitStatus: &ok, StartTS: time.Unix(10, 0).UTC()}
	if !(AdvancedFilter{}).Match(e) {
		t.Fatal("empty should match")
	}
	if (AdvancedFilter{CwdRE: mustRE(t, `/tmp`)}).Match(e) {
		t.Fatal("cwd should reject")
	}
	if !(AdvancedFilter{CommandRE: mustRE(t, `git`)}).Match(e) {
		t.Fatal("command should match")
	}
	e.Deleted = true
	if (AdvancedFilter{}).Match(e) {
		t.Fatal("deleted should not match")
	}
	e.Deleted = false
	e.ExitStatus = &fail
	if !(AdvancedFilter{ExitFailed: true}).Match(e) {
		t.Fatal("failed should match")
	}
}

func TestSummarizeRuns(t *testing.T) {
	ok := 0
	fail := 1
	runs := []Entry{
		{ID: "b", Command: "git", StartTS: time.Unix(20, 0).UTC(), Cwd: "/new", Hostname: "b", ExitStatus: &ok},
		{ID: "a", Command: "git", StartTS: time.Unix(10, 0).UTC(), Cwd: "/old", Hostname: "a", ExitStatus: &fail},
	}
	cs := SummarizeRuns("git", runs)
	if cs.Runs != 2 || cs.Success != 1 || cs.Failed != 1 || cs.LastCwd != "/new" || cs.FirstTS != time.Unix(10, 0).UTC() {
		t.Fatalf("%+v", cs)
	}
}

func TestRegexLiteralPrefix(t *testing.T) {
	if got := RegexLiteralPrefix("jj b set "); got != "jj b set " {
		t.Fatalf("literal %q", got)
	}
	if got := RegexLiteralPrefix("jj b set ("); got != "jj b set " {
		t.Fatalf("unclosed paren %q", got)
	}
	if got := RegexLiteralPrefix("^git "); got != "" {
		t.Fatalf("anchor %q", got)
	}
	if got := RegexLiteralPrefix("jj.*set"); got != "jj" {
		t.Fatalf("meta %q", got)
	}
}

func TestCommandNeedleInstr(t *testing.T) {
	s := testStore(t)
	ok := 0
	base := time.UnixMilli(1000).UTC()
	_, _ = s.Insert(Entry{ID: "1", Command: "jj b set (abc)", StartTS: base, DeviceID: "d", ExitStatus: &ok})
	_, _ = s.Insert(Entry{ID: "2", Command: "ls", StartTS: base.Add(time.Second), DeviceID: "d", ExitStatus: &ok})
	got, err := s.CommandSummariesAdvanced(AdvancedFilter{CommandNeedle: "jj b set", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "jj b set (abc)" {
		t.Fatalf("needle %+v", got)
	}
}
