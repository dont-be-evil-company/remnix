package device

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/db"
)

func TestRetireAndActiveIDs(t *testing.T) {
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewStore(d)
	if err := s.Upsert(Device{ID: "a", Name: "A", Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Device{ID: "b", Name: "B", Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := s.Retire("b", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	ids, err := s.ActiveIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "a" {
		t.Fatalf("%v", ids)
	}
	got, ok, err := s.Get("b")
	if err != nil || !ok || got.Status != StatusRetired {
		t.Fatalf("%v %v %+v", ok, err, got)
	}
}
