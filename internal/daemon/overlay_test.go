package daemon

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/helpparse"
	"github.com/dont-be-evil-company/remnix/internal/suggestcache"
	"github.com/dont-be-evil-company/remnix/internal/terminal"
	"github.com/dont-be-evil-company/remnix/internal/tui"
)

func TestNULSearchInteractiveUnknownSession(t *testing.T) {
	s := &Server{sessions: terminal.NewManager()}
	in := strings.NewReader("search-interactive\x00git\x00/tmp\x00missing-id\x00")
	var out bytes.Buffer
	if err := s.serveNULOne(bufio.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "err\x00") {
		t.Fatalf("want err, got %q", got)
	}
	if !strings.Contains(got, "unknown terminal session") {
		t.Fatalf("want unknown session, got %q", got)
	}
}

func TestNULSuggestInteractiveUnknownSession(t *testing.T) {
	s := &Server{sessions: terminal.NewManager()}
	in := strings.NewReader("suggest-interactive\x00git\x00/tmp\x00missing-id\x00")
	var out bytes.Buffer
	if err := s.serveNULOne(bufio.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "err\x00") {
		t.Fatalf("want err, got %q", out.String())
	}
}

func TestNULSuggestCompleteInteractiveUnknownSession(t *testing.T) {
	s := &Server{sessions: terminal.NewManager()}
	in := strings.NewReader("suggest-complete-interactive\x00git\x00/tmp\x00missing-id\x00\x002\x00git status\x00show the working tree\x00git stash\x00\x00")
	var out bytes.Buffer
	if err := s.serveNULOne(bufio.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "err\x00") {
		t.Fatalf("want err, got %q", out.String())
	}
}

func TestNULSuggestCompleteInteractiveAbortUnknownSession(t *testing.T) {
	s := &Server{sessions: terminal.NewManager()}
	in := strings.NewReader("suggest-complete-interactive\x00git\x00/tmp\x00missing-id\x00\x00-1\x00")
	var out bytes.Buffer
	if err := s.serveNULOne(bufio.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "err\x00") {
		t.Fatalf("want err, got %q", out.String())
	}
}

func TestSearchInteractiveSelectionFormat(t *testing.T) {
	if tui.FormatSelection("ls", true) != tui.AcceptPrefix+"ls" {
		t.Fatal("accept prefix")
	}
}

func TestSuggestCacheKeptOnContinue(t *testing.T) {
	s := &Server{}
	c := s.suggestCacheFor("s1")
	c.Store("git", []string{"git status"}, nil)
	if _, err := s.finishSuggestOverlay("s1", tui.ContinuePrefix+"git status", nil); err != nil {
		t.Fatal(err)
	}
	if s.suggestCaches["s1"] == nil {
		t.Fatal("continue must keep the session completion cache")
	}
	if !c.Has("git") {
		t.Fatal("cached items should remain after continue")
	}
}

func TestSuggestCacheDroppedOnInsert(t *testing.T) {
	s := &Server{}
	s.suggestCacheFor("s1").Store("git", []string{"git status"}, nil)
	if _, err := s.finishSuggestOverlay("s1", "git status", nil); err != nil {
		t.Fatal(err)
	}
	if s.suggestCaches["s1"] != nil {
		t.Fatal("insert must drop the session completion cache")
	}
}

func TestSuggestCacheDroppedOnError(t *testing.T) {
	s := &Server{}
	s.suggestCacheFor("s1").Store("git", []string{"git status"}, nil)
	if _, err := s.finishSuggestOverlay("s1", tui.ContinuePrefix+"git", io.EOF); err == nil {
		t.Fatal("want error")
	}
	if s.suggestCaches["s1"] != nil {
		t.Fatal("error must drop the session completion cache")
	}
}

func TestHelpDescrsEnrichedFromProbe(t *testing.T) {
	s := &Server{
		helpProbe: func(ctx context.Context, argv []string) (string, error) {
			return `Usage:
  tool [command]

Available Commands:
  daemon      Run the remnix core daemon
  help        Help about any command
`, nil
		},
	}
	got := s.enrichSuggestDescrs("s1", "tool ", []string{"tool daemon", "tool help"}, []string{"", "from compsys"})
	if got[0] != "Run the remnix core daemon" {
		t.Fatalf("daemon descr %q", got[0])
	}
	if got[1] != "from compsys" {
		t.Fatalf("compsys descr overwritten: %q", got[1])
	}
}

func TestHelpCacheKeptOnContinue(t *testing.T) {
	s := &Server{
		helpProbe: func(ctx context.Context, argv []string) (string, error) {
			return "Available Commands:\n  daemon      Run the remnix core daemon\n", nil
		},
	}
	_ = s.enrichSuggestDescrs("s1", "tool ", []string{"tool daemon"}, []string{""})
	if _, err := s.finishSuggestOverlay("s1", tui.ContinuePrefix+"tool daemon", nil); err != nil {
		t.Fatal(err)
	}
	if s.helpCaches["s1"] == nil {
		t.Fatal("continue must keep the session help cache")
	}
	probes := 0
	s.helpProbe = func(ctx context.Context, argv []string) (string, error) {
		probes++
		return "", nil
	}
	got := s.enrichSuggestDescrs("s1", "tool ", []string{"tool daemon"}, []string{""})
	if probes != 0 {
		t.Fatal("second enrich must use cached help")
	}
	if got[0] != "Run the remnix core daemon" {
		t.Fatalf("got %q", got[0])
	}
}

func TestHelpCacheDroppedOnInsert(t *testing.T) {
	s := &Server{
		helpProbe: func(ctx context.Context, argv []string) (string, error) {
			return "Available Commands:\n  daemon      Run the remnix core daemon\n", nil
		},
	}
	_ = s.enrichSuggestDescrs("s1", "tool ", []string{"tool daemon"}, []string{""})
	if _, err := s.finishSuggestOverlay("s1", "tool daemon", nil); err != nil {
		t.Fatal(err)
	}
	if s.helpCaches["s1"] != nil {
		t.Fatal("insert must drop the session help cache")
	}
}

func TestHelpNodesFilledFromChildProbe(t *testing.T) {
	s := &Server{
		helpProbe: func(ctx context.Context, argv []string) (string, error) {
			switch strings.Join(argv, " ") {
			case "aws":
				return "AVAILABLE SERVICES\n     * s3\n     * ec2\n", nil
			case "aws s3":
				return "NAME\n       s3 -\n\nDESCRIPTION\n       This command is used to manage Amazon S3.\n", nil
			default:
				return "", nil
			}
		},
	}
	items := []string{"aws s3", "aws ec2"}
	got := s.enrichSuggestDescrs("s1", "aws ", items, []string{"", ""})
	if got[0] != "" {
		t.Fatalf("parent star list invented descr %q", got[0])
	}
	got = helpparse.FillNodes(context.Background(), "aws ", items, got, s.helpCacheFor("s1"), s.helpProbe, 8, nil)
	if !strings.Contains(got[0], "manage Amazon S3") {
		t.Fatalf("s3 descr %q", got[0])
	}
}

func TestHelpDescrsFromDurableSkipProbe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "suggest-cache.db")
	store, err := suggestcache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ents := helpparse.Parse("Available Commands:\n  daemon      Run the remnix core daemon\n")
	if err := store.Save([]string{"tool"}, ents, ""); err != nil {
		t.Fatal(err)
	}
	s := &Server{suggestStore: store}
	s.helpProbe = func(ctx context.Context, argv []string) (string, error) {
		t.Fatal("warm L2 must not probe")
		return "", nil
	}
	got := s.enrichSuggestDescrs("s1", "tool ", []string{"tool daemon"}, []string{""})
	if got[0] != "Run the remnix core daemon" {
		t.Fatalf("got %q", got[0])
	}
}
