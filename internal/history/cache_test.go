package history

import (
	"context"
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/db"
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
