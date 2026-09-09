package history

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/db"
)

func TestCacheSuggestMatchesStore(t *testing.T) {
	d, err := db.OpenAndMigrate(t.TempDir() + "/h.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store := NewSQLStore(d.SQL)
	now := time.Now().UTC()
	entries := []Entry{
		{ID: "1", Command: "git status", StartTS: now, Cwd: "/a", DeviceID: "d1"},
		{ID: "2", Command: "git push", StartTS: now.Add(-time.Hour), Cwd: "/a", DeviceID: "d1"},
		{ID: "3", Command: "ls", StartTS: now, Cwd: "/b", DeviceID: "d2"},
	}
	for _, e := range entries {
		if _, err := store.Insert(e); err != nil {
			t.Fatal(err)
		}
	}
	cache := NewCache()
	if err := cache.Rebuild(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	got := cache.Suggest("git")
	if len(got) != 2 {
		t.Fatalf("got %d candidates: %+v", len(got), got)
	}
	sqlCands, err := store.SuggestPrefixCandidates("git", 64)
	if err != nil {
		t.Fatal(err)
	}
	if len(sqlCands) != len(got) {
		t.Fatalf("sql %d cache %d", len(sqlCands), len(got))
	}
}

func TestCacheTombstoneRemovesSuggestion(t *testing.T) {
	cache := NewCache()
	e := Entry{ID: "1", Command: "rm -rf /", StartTS: time.Now().UTC()}
	cache.ApplyCreated(e)
	if len(cache.Suggest("rm")) == 0 {
		t.Fatal("expected suggestion")
	}
	cache.ApplyTombstoneCommand("rm -rf /")
	if len(cache.Suggest("rm")) != 0 {
		t.Fatal("tombstoned command still suggested")
	}
}

func TestCacheFailedTransactionDoesNotApply(t *testing.T) {
	cache := NewCache()
	if cache.Dirty() {
		t.Fatal("new cache should be clean")
	}
	cache.MarkDirty()
	if !cache.Dirty() {
		t.Fatal("expected dirty")
	}
}

func TestChangeSetMerge(t *testing.T) {
	var a ChangeSet
	a.AddCreated(Entry{ID: "1", Command: "echo"})
	b := ChangeSet{}
	b.AddTombstoned("2")
	a.Merge(b)
	if len(a.Created) != 1 || len(a.Tombstoned) != 1 {
		t.Fatalf("%+v", a)
	}
}

func TestCacheCompactDropsDeadInterns(t *testing.T) {
	c := NewCache()
	now := time.Now().UTC()
	c.ApplyCreated(Entry{ID: "1", Command: "old-cmd", StartTS: now, Cwd: "/a", DeviceID: "dev", SessionID: "s1"})
	c.ApplyCreated(Entry{ID: "2", Command: "keep-cmd", StartTS: now, Cwd: "/b", DeviceID: "dev", SessionID: "s2"})
	c.ApplyTombstoneCommand("old-cmd")
	c.Compact()
	if got := c.Suggest("keep"); len(got) != 1 || got[0].Command != "keep-cmd" {
		t.Fatalf("keep: %+v", got)
	}
	if len(c.Suggest("old")) != 0 {
		t.Fatal("tombstoned command still suggested")
	}
	if n := c.Stats().InternedStrings; n > 4 {
		t.Fatalf("interned after compact=%d want <=4", n)
	}
}

func TestCacheDoesNotInternStaleSession(t *testing.T) {
	c := NewCache()
	now := time.Now().UTC()
	c.ApplyCreated(Entry{ID: "1", Command: "ls", StartTS: now, Cwd: "/a", DeviceID: "d", SessionID: "s1"})
	before := c.Stats().InternedStrings
	c.ApplyCreated(Entry{ID: "2", Command: "ls", StartTS: now.Add(-time.Hour), Cwd: "/z", DeviceID: "d2", SessionID: "s-old"})
	if after := c.Stats().InternedStrings; after != before {
		t.Fatalf("stale session interned: before=%d after=%d", before, after)
	}
	c.ApplyCreated(Entry{ID: "3", Command: "ls", StartTS: now.Add(time.Second), Cwd: "/b", DeviceID: "d", SessionID: "s2"})
	c.Compact()
	got := c.Suggest("l")
	if len(got) != 1 || got[0].SessionID != "s2" || got[0].Cwd != "/b" {
		t.Fatalf("%+v", got)
	}
}

func TestCacheCompactPrunesSessionChurn(t *testing.T) {
	c := NewCache()
	now := time.Now().UTC()
	for i := 0; i < 20; i++ {
		c.ApplyCreated(Entry{
			ID:        fmt.Sprintf("%d", i),
			Command:   "ls",
			StartTS:   now.Add(time.Duration(i) * time.Second),
			Cwd:       "/tmp",
			DeviceID:  "d",
			SessionID: fmt.Sprintf("sess-%d", i),
		})
	}
	c.Compact()
	if n := c.Stats().InternedStrings; n > 5 {
		t.Fatalf("interned=%d after compact, want live rec strings only", n)
	}
	got := c.Suggest("l")
	if len(got) != 1 || got[0].SessionID != "sess-19" {
		t.Fatalf("%+v", got)
	}
}
