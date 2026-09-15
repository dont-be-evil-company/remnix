package suggestcache

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/helpparse"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "suggest-cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStorePutGetPurge(t *testing.T) {
	s := testStore(t)
	ents := []helpparse.Entity{
		{Name: "storage", Descr: "Cloud Storage", Kind: helpparse.KindCommand},
		{Name: "--project", Descr: "Project id", Kind: helpparse.KindFlag},
	}
	if err := s.Save([]string{"gcloud"}, ents, "manage Google Cloud"); err != nil {
		t.Fatal(err)
	}
	got, sum, ok := s.Load([]string{"gcloud"})
	if !ok {
		t.Fatal("want page")
	}
	if sum != "manage Google Cloud" {
		t.Fatalf("summary %q", sum)
	}
	if len(got) != 2 {
		t.Fatalf("ents %d", len(got))
	}
	byName := map[string]helpparse.Entity{}
	for _, e := range got {
		byName[e.Name] = e
	}
	if byName["storage"].Descr != "Cloud Storage" || byName["storage"].Kind != helpparse.KindCommand {
		t.Fatalf("%+v", byName["storage"])
	}
	if byName["--project"].Kind != helpparse.KindFlag {
		t.Fatalf("%+v", byName["--project"])
	}

	if err := s.Save([]string{"gcloud", "storage"}, nil, "manage Cloud Storage buckets"); err != nil {
		t.Fatal(err)
	}
	pages := s.LoadMany([][]string{{"gcloud"}, {"gcloud", "storage"}, {"missing"}})
	if _, ok := pages[helpparse.ArgvKey([]string{"missing"})]; ok {
		t.Fatal("missing should not be a hit")
	}
	if pages[helpparse.ArgvKey([]string{"gcloud", "storage"})].Summary != "manage Cloud Storage buckets" {
		t.Fatalf("%+v", pages)
	}

	if err := s.Purge("gcloud"); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Load([]string{"gcloud"}); ok {
		t.Fatal("purged parent")
	}
	if _, _, ok := s.Load([]string{"gcloud", "storage"}); ok {
		t.Fatal("purged child")
	}
}

func TestStoreNegativeCache(t *testing.T) {
	s := testStore(t)
	if err := s.Save([]string{"aws"}, nil, ""); err != nil {
		t.Fatal(err)
	}
	ents, sum, ok := s.Load([]string{"aws"})
	if !ok {
		t.Fatal("empty page must still be a hit")
	}
	if len(ents) != 0 || sum != "" {
		t.Fatalf("ents=%v sum=%q", ents, sum)
	}
}

func TestStorePurgeAll(t *testing.T) {
	s := testStore(t)
	_ = s.Save([]string{"aws"}, nil, "a")
	_ = s.Save([]string{"gcloud"}, nil, "g")
	if err := s.PurgeAll(); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Load([]string{"aws"}); ok {
		t.Fatal("aws")
	}
	if _, _, ok := s.Load([]string{"gcloud"}); ok {
		t.Fatal("gcloud")
	}
}

func TestFillEmptyUsesDurableWithoutProbe(t *testing.T) {
	s := testStore(t)
	ents := helpparse.Parse("Available Commands:\n  daemon      Run the remnix core daemon\n")
	if err := s.Save([]string{"tool"}, ents, ""); err != nil {
		t.Fatal(err)
	}
	probes := 0
	probe := func(ctx context.Context, argv []string) (string, error) {
		probes++
		return "", nil
	}
	cache := helpparse.NewCache()
	cache.SetDurable(s)
	got := helpparse.FillEmpty(context.Background(), "tool ", []string{"tool daemon"}, []string{""}, cache, probe)
	if probes != 0 {
		t.Fatalf("probes %d, want 0 (L2)", probes)
	}
	if got[0] != "Run the remnix core daemon" {
		t.Fatalf("got %q", got[0])
	}
}

func TestFillWritebackThenNewSessionDoesNotProbe(t *testing.T) {
	s := testStore(t)
	probes := 0
	probe := func(ctx context.Context, argv []string) (string, error) {
		probes++
		return `Usage:
  tool [command]

Available Commands:
  daemon      Run the remnix core daemon
`, nil
	}
	c1 := helpparse.NewCache()
	c1.SetDurable(s)
	got := helpparse.FillEmpty(context.Background(), "tool ", []string{"tool daemon"}, []string{""}, c1, probe)
	if probes != 1 {
		t.Fatalf("probes %d", probes)
	}
	if got[0] != "Run the remnix core daemon" {
		t.Fatalf("got %q", got[0])
	}
	c2 := helpparse.NewCache()
	c2.SetDurable(s)
	got = helpparse.FillEmpty(context.Background(), "tool ", []string{"tool daemon"}, []string{""}, c2, probe)
	if probes != 1 {
		t.Fatalf("second session probed: %d", probes)
	}
	if got[0] != "Run the remnix core daemon" {
		t.Fatalf("got %q", got[0])
	}
}

func TestFillNodesLooksUpChildPages(t *testing.T) {
	s := testStore(t)
	_ = s.Save([]string{"aws"}, nil, "")
	_ = s.Save([]string{"aws", "s3"}, nil, "This command is used to manage Amazon S3.")
	probes := 0
	probe := func(ctx context.Context, argv []string) (string, error) {
		probes++
		return "", nil
	}
	cache := helpparse.NewCache()
	cache.SetDurable(s)
	got := helpparse.FillNodes(context.Background(), "aws ", []string{"aws s3", "aws ec2"}, []string{"", ""}, cache, probe, 8, nil)
	if probes != 1 {
		t.Fatalf("only the L2 miss should probe, probes=%d", probes)
	}
	if !strings.Contains(got[0], "manage Amazon S3") {
		t.Fatalf("got %q", got[0])
	}
}

func TestWarmupWalksCommandsNotFlags(t *testing.T) {
	s := testStore(t)
	pages := map[string]string{
		"tool": `Available Commands:
  storage     Cloud Storage
  help        Help about any command

Flags:
  --verbose   more output
`,
		"tool storage": `NAME
    storage - Cloud Storage command group

COMMANDS
  buckets     manage buckets

Flags:
  --json   json output
`,
		"tool storage buckets": `NAME
    buckets - manage buckets

DESCRIPTION
    Create and list buckets.
`,
	}
	var probeMu sync.Mutex
	probed := []string{}
	probe := func(ctx context.Context, argv []string) (string, error) {
		key := strings.Join(argv, " ")
		probeMu.Lock()
		probed = append(probed, key)
		probeMu.Unlock()
		if h, ok := pages[key]; ok {
			return h, nil
		}
		return "", nil
	}
	var out strings.Builder
	if err := Warmup(context.Background(), s, []string{"tool"}, WarmupOptions{
		Probe: probe,
		Jobs:  2,
		Out:   &out,
	}); err != nil {
		t.Fatal(err)
	}
	for _, p := range probed {
		if strings.Contains(p, "help") || strings.Contains(p, "--") {
			t.Fatalf("probed noise %q in %v", p, probed)
		}
	}
	if _, _, ok := s.Load([]string{"tool", "storage", "buckets"}); !ok {
		t.Fatal("want buckets page")
	}
	ents, _, ok := s.Load([]string{"tool"})
	if !ok {
		t.Fatal("want tool page")
	}
	hasFlag := false
	for _, e := range ents {
		if e.Kind == helpparse.KindFlag || strings.HasPrefix(e.Name, "-") {
			hasFlag = true
		}
	}
	if !hasFlag {
		t.Fatal("flags must be stored as entities")
	}
	if _, _, ok := s.Load([]string{"tool", "help"}); ok {
		t.Fatal("must not recurse into help")
	}
	if _, _, ok := s.Load([]string{"tool", "--verbose"}); ok {
		t.Fatal("must not recurse into flags")
	}
	if !strings.Contains(out.String(), "warming tool") {
		t.Fatalf("progress %q", out.String())
	}
}

func TestWarmupDepth(t *testing.T) {
	s := testStore(t)
	probe := func(ctx context.Context, argv []string) (string, error) {
		return "Available Commands:\n  storage     Cloud Storage\n", nil
	}
	if err := Warmup(context.Background(), s, []string{"tool"}, WarmupOptions{Probe: probe, Depth: 1}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Load([]string{"tool"}); !ok {
		t.Fatal("root")
	}
	if _, _, ok := s.Load([]string{"tool", "storage"}); ok {
		t.Fatal("depth 1 must not probe children")
	}
}

func TestWarmupToolDefaultsNilProbe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell stub")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "mytool")
	script := "#!/bin/sh\nprintf '%s\\n' 'Available Commands:' '  foo  does foo'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	s := testStore(t)
	if err := warmupTool(context.Background(), s, "mytool", WarmupOptions{Jobs: 1, Depth: 1}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Load([]string{"mytool"}); !ok {
		t.Fatal("root page")
	}
}
