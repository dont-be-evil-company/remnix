package daemon

import (
	"bufio"
	"bytes"
	"strings"
	"testing"

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
	in := strings.NewReader("suggest-complete-interactive\x00git\x00/tmp\x00missing-id\x002\x00git status\x00show the working tree\x00git stash\x00\x00")
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
	in := strings.NewReader("suggest-complete-interactive\x00git\x00/tmp\x00missing-id\x00-1\x00")
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
