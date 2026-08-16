package importers

import (
	"strings"
	"testing"
	"time"
)

func TestParseZsh(t *testing.T) {
	in := ": 1700000000:0;ls -la\n: 1700000001:2;git status\n"
	recs, err := ParseHistfile(strings.NewReader(in), "zsh")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].Command != "ls -la" {
		t.Fatalf("%+v", recs)
	}
	if recs[0].StartTS.Unix() != 1700000000 {
		t.Fatalf("ts %v", recs[0].StartTS)
	}
}

func TestParseBashTimestamps(t *testing.T) {
	in := "#1700000000\necho hi\n#1700000001\ncd /tmp\n"
	recs, err := ParseHistfile(strings.NewReader(in), "bash")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[1].Command != "cd /tmp" {
		t.Fatalf("%+v", recs)
	}
}

func TestParseFish(t *testing.T) {
	in := "- cmd: cargo test\n  when: 1700000000\n  paths:\n    - /proj\n"
	recs, err := ParseHistfile(strings.NewReader(in), "fish")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].Command != "cargo test" {
		t.Fatalf("%+v", recs)
	}
	if recs[0].StartTS.Unix() != 1700000000 {
		t.Fatalf("ts %v", recs[0].StartTS)
	}
}

func TestDetectKind(t *testing.T) {
	if DetectKind("/home/x/.zsh_history", ": 1:0;ls") != "zsh" {
		t.Fatal("zsh")
	}
	if DetectKind("unknown", "- cmd: ls") != "fish" {
		t.Fatal("fish")
	}
}

func TestToEntriesSkipsEmpty(t *testing.T) {
	got := ToEntries([]Record{{Command: " ", StartTS: time.Unix(1, 0)}, {Command: "true", StartTS: time.Unix(2, 0)}}, "dev", "zsh")
	if len(got) != 1 {
		t.Fatalf("%d", len(got))
	}
}
