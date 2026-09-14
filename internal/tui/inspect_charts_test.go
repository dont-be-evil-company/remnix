package tui

import (
	"testing"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/history"
)

func TestBucketRunsGranularity(t *testing.T) {
	ok := 0
	fail := 1
	runs := []history.Entry{
		{StartTS: time.Date(2025, 12, 31, 23, 0, 0, 0, time.UTC), ExitStatus: &ok},
		{StartTS: time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC), ExitStatus: &fail},
		{StartTS: time.Date(2026, 1, 2, 1, 0, 0, 0, time.UTC), ExitStatus: &ok},
		{StartTS: time.Date(2026, 2, 1, 1, 0, 0, 0, time.UTC), ExitStatus: &ok},
	}
	days := bucketRuns(runs, chartDay)
	if len(days) != 4 || days[0].Label != "2025-12-31" || days[1].Total != 1 || days[1].Fail != 1 {
		t.Fatalf("days %+v", days)
	}
	months := bucketRuns(runs, chartMonth)
	if len(months) != 3 || months[0].Label != "2025-12" || months[1].Label != "2026-01" || months[1].Total != 2 {
		t.Fatalf("months %+v", months)
	}
	years := bucketRuns(runs, chartYear)
	if len(years) != 2 || years[0].Label != "2025" || years[1].Label != "2026" || years[1].Total != 3 {
		t.Fatalf("years %+v", years)
	}
}

func TestTopByHostAndCwd(t *testing.T) {
	ok := 0
	fail := 1
	runs := []history.Entry{
		{Hostname: "dev", Cwd: "/repo", ExitStatus: &ok},
		{Hostname: "dev", Cwd: "/repo", ExitStatus: &fail},
		{Hostname: "dev", Cwd: "/old", ExitStatus: &ok},
		{Hostname: "laptop", Cwd: "/repo", ExitStatus: &ok},
		{Hostname: "", Cwd: "", ExitStatus: &ok},
	}
	hosts := topBy(runs, func(e history.Entry) string { return e.Hostname }, 10)
	if len(hosts) != 3 || hosts[0].Label != "dev" || hosts[0].Total != 3 || hosts[0].Fail != 1 {
		t.Fatalf("hosts %+v", hosts)
	}
	if hosts[1].Label != "(none)" || hosts[2].Label != "laptop" {
		t.Fatalf("host order %+v", hosts)
	}
	cwds := topBy(runs, func(e history.Entry) string { return e.Cwd }, 10)
	if len(cwds) != 3 || cwds[0].Label != "/repo" || cwds[0].Total != 3 {
		t.Fatalf("cwds %+v", cwds)
	}
	if cwds[1].Label != "(none)" || cwds[2].Label != "/old" {
		t.Fatalf("cwd order %+v", cwds)
	}
}

func TestChartGranularityCycles(t *testing.T) {
	g := chartMonth
	if g.Next() != chartDay || g.Next().Next() != chartYear || g.Next().Next().Next() != chartMonth {
		t.Fatalf("cycle %v %v %v", g.Next(), g.Next().Next(), g.Next().Next().Next())
	}
}
