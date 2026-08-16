package device

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/db"
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

func TestUpsertDoesNotUnretire(t *testing.T) {
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewStore(d)
	if err := s.Upsert(Device{ID: "b", Name: "B", Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.Retire("b", at); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(Device{ID: "b", Name: "B-renamed", Hostname: "host", Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get("b")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.Status != StatusRetired {
		t.Fatalf("status %q", got.Status)
	}
	if got.Name != "B-renamed" || got.Hostname != "host" {
		t.Fatalf("name/host %+v", got)
	}
	if got.RetiredAt == nil || !got.RetiredAt.Equal(at) {
		t.Fatalf("retired_at %+v want %v", got.RetiredAt, at)
	}
}

func TestDeleteRemovesDevice(t *testing.T) {
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewStore(d)
	if err := s.Upsert(Device{ID: "b", Name: "B", Status: StatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("b"); err != nil {
		t.Fatal(err)
	}
	_, ok, err := s.Get("b")
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if err := s.Delete("b"); err == nil {
		t.Fatal("expected not found")
	}
}
