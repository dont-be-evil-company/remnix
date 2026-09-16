package helpparse

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func entityMap(ents []Entity) map[string]string {
	m := map[string]string{}
	for _, e := range ents {
		m[e.Name] = e.Descr
	}
	return m
}

func TestIdentifySignatures(t *testing.T) {
	cases := []struct {
		text string
		want Framework
	}{
		{"usage: tool [-h]\n\noptions:\n  -h, --help  show this help message and exit\n", Argparse},
		{"Usage: tool [OPTIONS]\n\nOptions:\n  --help  Show this message and exit.\n", Click},
		{"Usage:\n  tool [command]\n\nAvailable Commands:\n  help  Help about any command\n", Cobra},
		{"Usage: tool [OPTION...]\n\n  -v, --verbose  be verbose\n\nMandatory arguments to long options are mandatory for short options too.\n", GnuArgp},
		{"BusyBox is copyrighted software licensed under the GNU General Public License.\n\nUsage: busybox [function] [arguments]...\n", Busybox},
		{"Usage of tool:\n  -v\tbe verbose\n", GoFlag},
		{"usage: ls [-ABCFGHLOPRSTUWabcdefghiklmnopqrstuwx1] [file ...]\n", BsdTerse},
		{"usage: tool [-v] [--verbose]\n", Unknown},
		{"", Unknown},
	}
	for _, tc := range cases {
		if got := Identify(tc.text); got != tc.want {
			t.Errorf("Identify(%q) = %q, want %q", tc.text[:min(60, len(tc.text))], got, tc.want)
		}
	}
	if Identify("Usage:\n docker [OPTIONS] COMMAND\n\nCommon Commands:\n  run  Create and run a new container\n") == Cobra {
		t.Fatal("Common Commands: must not be a cobra signature")
	}
}

func TestParseCobraFixture(t *testing.T) {
	ents := Parse(readFixture(t, "cobra.txt"))
	if Identify(readFixture(t, "cobra.txt")) != Cobra {
		t.Fatal("fixture should identify as cobra")
	}
	m := entityMap(ents)
	for name, descr := range map[string]string{
		"completion": "Generate the autocompletion script for the specified shell",
		"daemon":     "Run the remnix core daemon",
		"help":       "Help about any command",
		"-h":         "help for tool",
		"--help":     "help for tool",
		"--verbose":  "more output",
	} {
		if m[name] != descr {
			t.Errorf("%s: got %q want %q (ents=%v)", name, m[name], descr, ents)
		}
	}
}

func TestParseArgparseFixture(t *testing.T) {
	text := readFixture(t, "argparse.txt")
	if Identify(text) != Argparse {
		t.Fatal("fixture should identify as argparse")
	}
	m := entityMap(Parse(text))
	if m["auth"] != "Manage credentials" || m["compute"] != "Create and manipulate Compute Engine resources" {
		t.Fatalf("commands: %+v", m)
	}
	if m["--project"] != "Google Cloud project ID" {
		t.Fatalf("--project: %q", m["--project"])
	}
	if _, ok := m["{auth,compute}"]; ok {
		t.Fatal("must not fabricate brace-alternation commands")
	}
}

func TestParseGcloudGroupsSlice(t *testing.T) {
	m := entityMap(Parse(readFixture(t, "gcloud_groups.txt")))
	want := map[string]string{
		"auth":                   "Manage oauth2 credentials for the Google Cloud CLI.",
		"access-approval":        "Manage Access Approval requests and settings.",
		"access-context-manager": "Manage Access Context Manager resources.",
		"ai":                     "Manage entities in Vertex AI.",
	}
	for name, descr := range want {
		if m[name] != descr {
			t.Errorf("%s: got %q want %q", name, m[name], descr)
		}
	}
}

func TestParseAWSStarBullets(t *testing.T) {
	m := entityMap(Parse(readFixture(t, "aws_star.txt")))
	for _, name := range []string{"accessanalyzer", "acm"} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing %s in %d names", name, len(m))
		}
		if m[name] != "" {
			t.Errorf("star list must not invent descr for %s: %q", name, m[name])
		}
	}
}

func TestParseCalliopeFixture(t *testing.T) {
	m := entityMap(Parse(readFixture(t, "calliope.txt")))
	want := map[string]string{
		"auth":    "Manage oauth2 credentials for the cloud CLI.",
		"compute": "Create and manipulate Compute Engine resources.",
		"help":    "Search help text.",
		"version": "Print version information.",
		"config":  "View and edit configuration",
		"storage": "Upload and download objects",
	}
	for name, descr := range want {
		if m[name] != descr {
			t.Errorf("%s: got %q want %q", name, m[name], descr)
		}
	}
}

func TestParseAWSBareDoesNotFabricateDescriptions(t *testing.T) {
	ents := Parse(readFixture(t, "aws_bare.txt"))
	m := entityMap(ents)
	for _, name := range []string{"accessanalyzer", "acm", "s3"} {
		if _, ok := m[name]; !ok {
			t.Errorf("missing name %s in %+v", name, m)
		}
		if m[name] != "" {
			t.Errorf("bare list must not invent a description for %s: %q", name, m[name])
		}
	}
}

func TestParseAWSDescribedCommands(t *testing.T) {
	m := entityMap(Parse(readFixture(t, "aws_described.txt")))
	if !strings.Contains(m["cp"], "Copies a local file") {
		t.Fatalf("cp: %q", m["cp"])
	}
	if !strings.Contains(m["ls"], "List S3 objects") {
		t.Fatalf("ls: %q", m["ls"])
	}
	if !strings.Contains(m["mb"], "Creates an S3 bucket") {
		t.Fatalf("mb: %q", m["mb"])
	}
}

func TestEnrichDoesNotOverwrite(t *testing.T) {
	ents := []Entity{{Name: "daemon", Descr: "from help"}}
	got := Enrich("tool ", []string{"tool daemon", "tool help"}, []string{"from compsys", ""}, ents)
	if got[0] != "from compsys" {
		t.Fatalf("overwrote compsys: %q", got[0])
	}
	if got[1] != "" {
		t.Fatalf("help was not in entities with descr, got %q", got[1])
	}
	got = Enrich("tool ", []string{"tool daemon"}, []string{""}, ents)
	if got[0] != "from help" {
		t.Fatalf("missing fill: %q", got[0])
	}
}

func TestHelpArgvDropsPartialToken(t *testing.T) {
	if got := HelpArgv("gcloud"); strings.Join(got, " ") != "gcloud" {
		t.Fatalf("got %v", got)
	}
	if got := HelpArgv("gcloud "); strings.Join(got, " ") != "gcloud" {
		t.Fatalf("got %v", got)
	}
	if got := HelpArgv("gcloud storage"); strings.Join(got, " ") != "gcloud" {
		t.Fatalf("got %v", got)
	}
	if got := HelpArgv("gcloud storage "); strings.Join(got, " ") != "gcloud storage" {
		t.Fatalf("got %v", got)
	}
	if HelpArgv("echo hi; rm") != nil {
		t.Fatal("unsafe argv")
	}
}

func TestFillEmptyUsesCacheAndProbe(t *testing.T) {
	cache := NewCache()
	probes := 0
	probe := func(ctx context.Context, argv []string) (string, error) {
		probes++
		return readFixture(t, "cobra.txt"), nil
	}
	items := []string{"tool daemon", "tool help"}
	got := FillEmpty(context.Background(), "tool ", items, []string{"", ""}, cache, probe)
	if got[0] != "Run the remnix core daemon" {
		t.Fatalf("got %v", got)
	}
	_ = FillEmpty(context.Background(), "tool ", items, []string{"", ""}, cache, probe)
	if probes != 1 {
		t.Fatalf("probes %d, want 1 (cache)", probes)
	}
}

func TestFillEmptySkipsWhenDescribed(t *testing.T) {
	probes := 0
	probe := func(ctx context.Context, argv []string) (string, error) {
		probes++
		return "", nil
	}
	got := FillEmpty(context.Background(), "tool ", []string{"tool daemon"}, []string{"already"}, NewCache(), probe)
	if probes != 0 || got[0] != "already" {
		t.Fatalf("probes=%d got=%v", probes, got)
	}
}

func TestItemsFromCobraHelp(t *testing.T) {
	cache := NewCache()
	probes := 0
	probe := func(ctx context.Context, argv []string) (string, error) {
		probes++
		if strings.Join(argv, " ") != "tool" {
			t.Fatalf("argv %v", argv)
		}
		return readFixture(t, "cobra.txt"), nil
	}
	items, descrs := Items(context.Background(), "tool ", cache, probe)
	byItem := map[string]string{}
	for i, item := range items {
		byItem[item] = descrs[i]
	}
	if byItem["tool daemon"] != "Run the remnix core daemon" {
		t.Fatalf("daemon: %q items=%v", byItem["tool daemon"], items)
	}
	if byItem["tool --verbose"] != "more output" {
		t.Fatalf("--verbose: %q", byItem["tool --verbose"])
	}
	if _, ok := byItem["tool help"]; !ok {
		t.Fatalf("missing help: %v", items)
	}
	partial, _ := Items(context.Background(), "tool da", cache, probe)
	if probes != 1 {
		t.Fatalf("probes %d, want 1 (cache; partial still uses parent page)", probes)
	}
	if len(partial) != len(items) {
		t.Fatalf("partial prefix must return full sibling list: %d vs %d", len(partial), len(items))
	}
}

func TestItemsFromArgparseHelp(t *testing.T) {
	items, descrs := Items(context.Background(), "tool ", NewCache(), func(ctx context.Context, argv []string) (string, error) {
		return readFixture(t, "argparse.txt"), nil
	})
	byItem := map[string]string{}
	for i, item := range items {
		byItem[item] = descrs[i]
	}
	if byItem["tool auth"] != "Manage credentials" || byItem["tool --project"] != "Google Cloud project ID" {
		t.Fatalf("got %v", byItem)
	}
}

func TestItemsEmptyPrefix(t *testing.T) {
	probe := func(ctx context.Context, argv []string) (string, error) {
		t.Fatal("empty prefix must not probe")
		return "", nil
	}
	items, descrs := Items(context.Background(), "", NewCache(), probe)
	if items != nil || descrs != nil {
		t.Fatalf("got %v %v", items, descrs)
	}
	if items, _ = Items(context.Background(), "echo hi; rm", NewCache(), probe); items != nil {
		t.Fatal("unsafe argv")
	}
}

func TestCompleteOrHistoryPrefersCompsys(t *testing.T) {
	probe := func(ctx context.Context, argv []string) (string, error) {
		t.Fatal("compsys items must not probe")
		return "", nil
	}
	items, descrs := CompleteOrHistory(context.Background(), "tool ", []string{"tool daemon"}, []string{"from compsys"}, NewCache(), probe, func() []string {
		t.Fatal("compsys items must not use history")
		return []string{"tool from-history"}
	})
	if len(items) != 1 || items[0] != "tool daemon" || descrs[0] != "from compsys" {
		t.Fatalf("got %v %v", items, descrs)
	}
}

func TestCompleteOrHistoryPrefersHelpOverHistory(t *testing.T) {
	probe := func(ctx context.Context, argv []string) (string, error) {
		return readFixture(t, "cobra.txt"), nil
	}
	items, descrs := CompleteOrHistory(context.Background(), "tool ", nil, nil, NewCache(), probe, func() []string {
		t.Fatal("help items must not fall through to history")
		return []string{"tool from-history"}
	})
	if len(items) == 0 {
		t.Fatal("want help items")
	}
	found := false
	for i, item := range items {
		if item == "tool daemon" && descrs[i] == "Run the remnix core daemon" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing daemon: %v %v", items, descrs)
	}
}

func TestCompleteOrHistoryFallsBackToHistory(t *testing.T) {
	probe := func(ctx context.Context, argv []string) (string, error) {
		return "", fmt.Errorf("no help")
	}
	items, descrs := CompleteOrHistory(context.Background(), "tool ", nil, nil, NewCache(), probe, func() []string {
		return []string{"tool from-history"}
	})
	if len(items) != 1 || items[0] != "tool from-history" || descrs != nil {
		t.Fatalf("got %v %v", items, descrs)
	}
}

func TestPickStreamPrefersHelpOnStderr(t *testing.T) {
	stdout := "error: unknown flag\n"
	stderr := "Usage: ip [OPTIONS]\n\nOptions:\n  -h, --help  show this help\n"
	got := pickStream(stdout, stderr)
	if !strings.Contains(got, "Usage: ip") {
		t.Fatalf("got %q", got)
	}
}

func TestDescribedCommandsIgnoresFlagsAndBareNames(t *testing.T) {
	if n := describedCommands([]Entity{
		{Name: "--help", Descr: "show help"},
		{Name: "auth", Descr: "Manage credentials"},
		{Name: "s3", Descr: ""},
	}); n != 1 {
		t.Fatalf("n=%d", n)
	}
}

func TestHelpArgvVariantsOrder(t *testing.T) {
	got := helpArgvVariants([]string{"aws"})
	if len(got) != 3 || got[0][1] != "--help" || got[1][1] != "help" || got[2][1] != "-h" {
		t.Fatalf("%v", got)
	}
}

func TestNodeSummaryAWSDescription(t *testing.T) {
	got := NodeSummary(readFixture(t, "aws_described.txt"))
	if got != "This command is used to manage Amazon S3." {
		t.Fatalf("got %q", got)
	}
}

func TestNodeSummaryPrefersNAMEDash(t *testing.T) {
	help := `NAME
    compute - Create and manipulate Compute Engine resources

SYNOPSIS
    compute GROUP | COMMAND [flags]

DESCRIPTION
    A longer paragraph that should not replace the NAME one-liner. It keeps going.
`
	got := NodeSummary(help)
	if got != "Create and manipulate Compute Engine resources" {
		t.Fatalf("got %q", got)
	}
}

func TestNodeSummaryEmptyNAMEUsesDescription(t *testing.T) {
	help := `NAME
       s3 -

DESCRIPTION
       This command is used to manage Amazon S3.

AVAILABLE COMMANDS
       * cp
`
	got := NodeSummary(help)
	if got != "This command is used to manage Amazon S3." {
		t.Fatalf("got %q", got)
	}
}

func TestNodeSummaryEmptyOnCommandList(t *testing.T) {
	if got := NodeSummary(readFixture(t, "cobra.txt")); got != "" {
		t.Fatalf("command lists must not become a node summary: %q", got)
	}
	if got := NodeSummary(readFixture(t, "aws_star.txt")); got != "" {
		t.Fatalf("star lists must not become a node summary: %q", got)
	}
}

func TestItemArgv(t *testing.T) {
	if got := strings.Join(ItemArgv("aws ", "aws s3"), " "); got != "aws s3" {
		t.Fatalf("full line: %q", got)
	}
	if got := strings.Join(ItemArgv("aws ", "s3"), " "); got != "aws s3" {
		t.Fatalf("bare token: %q", got)
	}
	if ItemArgv("aws ", "aws") != nil {
		t.Fatal("parent row must not be probed as a child")
	}
	if ItemArgv("aws ", "aws --debug") != nil {
		t.Fatal("flag rows must be skipped")
	}
}

func TestFillNodesFillsFromChildHelp(t *testing.T) {
	var mu sync.Mutex
	probes := []string{}
	probe := func(ctx context.Context, argv []string) (string, error) {
		mu.Lock()
		probes = append(probes, strings.Join(argv, " "))
		mu.Unlock()
		switch strings.Join(argv, " ") {
		case "aws":
			return readFixture(t, "aws_star.txt"), nil
		case "aws s3":
			return readFixture(t, "aws_described.txt"), nil
		default:
			return "", nil
		}
	}
	items := []string{"aws s3", "aws ec2"}
	cache := NewCache()
	got := FillEmpty(context.Background(), "aws ", items, []string{"", ""}, cache, probe)
	if got[0] != "" || got[1] != "" {
		t.Fatalf("star list must not invent descrs: %v", got)
	}
	var updates atomic.Int32
	got = FillNodes(context.Background(), "aws ", items, got, cache, probe, 8, func([]string) { updates.Add(1) })
	if !strings.Contains(got[0], "manage Amazon S3") {
		t.Fatalf("s3 descr %q", got[0])
	}
	if got[1] != "" {
		t.Fatalf("failed child must stay empty, got %q", got[1])
	}
	if updates.Load() != 1 {
		t.Fatalf("updates %d", updates.Load())
	}
	nAWS, nS3 := 0, 0
	mu.Lock()
	for _, p := range probes {
		switch p {
		case "aws":
			nAWS++
		case "aws s3":
			nS3++
		}
	}
	if nAWS != 1 || nS3 != 1 {
		t.Fatalf("probes %v", probes)
	}
	mu.Unlock()
	_ = FillNodes(context.Background(), "aws ", items, []string{"", ""}, cache, probe, 8, nil)
	nS3 = 0
	mu.Lock()
	for _, p := range probes {
		if p == "aws s3" {
			nS3++
		}
	}
	mu.Unlock()
	if nS3 != 1 {
		t.Fatalf("child help must be cached, probes %v", probes)
	}
}

func TestParseSetsKind(t *testing.T) {
	ents := Parse(readFixture(t, "cobra.txt"))
	byName := map[string]Entity{}
	for _, e := range ents {
		byName[e.Name] = e
	}
	if byName["daemon"].Kind != KindCommand {
		t.Fatalf("daemon kind %q", byName["daemon"].Kind)
	}
	if byName["--help"].Kind != KindFlag {
		t.Fatalf("--help kind %q", byName["--help"].Kind)
	}
}

func TestFillNodesDoesNotOverwrite(t *testing.T) {
	probe := func(ctx context.Context, argv []string) (string, error) {
		t.Fatal("must not probe described rows")
		return "", nil
	}
	got := FillNodes(context.Background(), "aws ", []string{"aws s3"}, []string{"from compsys"}, NewCache(), probe, 8, nil)
	if got[0] != "from compsys" {
		t.Fatalf("got %q", got[0])
	}
}

func TestFillNodesRespectsLimit(t *testing.T) {
	var probes atomic.Int32
	probe := func(ctx context.Context, argv []string) (string, error) {
		probes.Add(1)
		return readFixture(t, "aws_described.txt"), nil
	}
	items := []string{"aws s3", "aws ec2", "aws iam"}
	got := FillNodes(context.Background(), "aws ", items, []string{"", "", ""}, NewCache(), probe, 1, nil)
	if probes.Load() != 1 {
		t.Fatalf("probes %d, want 1", probes.Load())
	}
	if !strings.Contains(got[0], "manage Amazon S3") {
		t.Fatalf("first row %q", got[0])
	}
	if got[1] != "" || got[2] != "" {
		t.Fatalf("later rows %v", got)
	}
}

func TestFillNodesUnlimitedWhenLimitZero(t *testing.T) {
	var probes atomic.Int32
	probe := func(ctx context.Context, argv []string) (string, error) {
		probes.Add(1)
		return readFixture(t, "aws_described.txt"), nil
	}
	items := []string{"aws s3", "aws ec2", "aws iam"}
	got := FillNodes(context.Background(), "aws ", items, []string{"", "", ""}, NewCache(), probe, 0, nil)
	if probes.Load() != 3 {
		t.Fatalf("probes %d, want 3", probes.Load())
	}
	for i, d := range got {
		if !strings.Contains(d, "manage Amazon S3") {
			t.Fatalf("row %d %q", i, d)
		}
	}
}

func TestFillNodesDoesNotPersistCanceledProbe(t *testing.T) {
	started := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	probe := func(ctx context.Context, argv []string) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	go func() {
		<-started
		cancel()
	}()
	cache := NewCache()
	_ = FillNodes(ctx, "aws ", []string{"aws s3"}, []string{""}, cache, probe, 0, nil)
	if _, ok := cache.GetSummary([]string{"aws", "s3"}); ok {
		t.Fatal("canceled probe must not persist an empty page")
	}
}

func TestHelpPageRankPrefersDescription(t *testing.T) {
	stub := "usage: aws [options] <command>\n\nNeed to specify a command.\n"
	page := readFixture(t, "aws_described.txt")
	if helpPageRank(stub, describedCommands(Parse(stub))) >= helpPageRank(page, describedCommands(Parse(page))) {
		t.Fatal("DESCRIPTION page must outrank a usage stub")
	}
}

func TestLookPathUsesCallerPATH(t *testing.T) {
	dir := t.TempDir()
	name := "remnix-help-probe-bin"
	bin := filepath.Join(dir, name)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := lookPath(name, filepath.Join(dir, "missing")); got == bin {
		t.Fatal("must not resolve a binary that is not on PATH")
	}
	got := lookPath(name, dir+string(os.PathListSeparator)+"/usr/bin")
	if got != bin {
		t.Fatalf("got %q want %q", got, bin)
	}
}

func TestProbeEnvReplacesPATH(t *testing.T) {
	env := probeEnv("/opt/user/bin:/usr/bin")
	var path, awsPager string
	nPATH := 0
	for _, e := range env {
		switch {
		case strings.HasPrefix(e, "PATH="):
			nPATH++
			path = strings.TrimPrefix(e, "PATH=")
		case strings.HasPrefix(e, "AWS_PAGER="):
			awsPager = strings.TrimPrefix(e, "AWS_PAGER=")
		}
	}
	if nPATH != 1 || path != "/opt/user/bin:/usr/bin" {
		t.Fatalf("PATH copies=%d value=%q", nPATH, path)
	}
	if awsPager != "cat" {
		t.Fatalf("AWS_PAGER=%q", awsPager)
	}
}

func TestProbeGoHelp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("go --help probe")
	}
	out, err := Probe(context.Background(), []string{"go"})
	if err != nil {
		t.Fatal(err)
	}
	if Identify(out) == Unknown && len(Parse(out)) == 0 && !strings.Contains(strings.ToLower(out), "usage") {
		t.Fatalf("go --help not recognized as help: %q", out[:min(200, len(out))])
	}
}
