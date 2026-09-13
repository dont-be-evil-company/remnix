package keys

import (
	"path/filepath"
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/crypto/generations"
	"github.com/dont-be-evil-company/remnix/internal/db"
)

func TestStageAndPromoteRotation(t *testing.T) {
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewStore(d)
	old := generations.Manifest{GenerationID: "g1", Seq: 1, Active: true}
	if err := s.PutGeneration(old); err != nil {
		t.Fatal(err)
	}
	next := generations.Manifest{GenerationID: "g2", Seq: 2, Active: true}
	if err := s.StageRotation(next, old, "remnix1test"); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Generation("g2")
	if err != nil || !ok || got.Active {
		t.Fatalf("staged generation must not be active: ok=%v err=%v active=%v", ok, err, got.Active)
	}
	rot, ok, err := s.Rotation()
	if err != nil || !ok || rot.Phase != RotationPrepared || rot.NewGenerationID != "g2" {
		t.Fatalf("journal %+v ok=%v err=%v", rot, ok, err)
	}
	active, ok, err := s.Active()
	if err != nil || !ok || active.GenerationID != "g1" {
		t.Fatalf("old must stay active: %+v ok=%v err=%v", active, ok, err)
	}
	if err := s.PromoteRotation("g1", "g2"); err != nil {
		t.Fatal(err)
	}
	active, ok, err = s.Active()
	if err != nil || !ok || active.GenerationID != "g2" {
		t.Fatalf("new should be active: %+v", active)
	}
	oldGot, ok, err := s.Generation("g1")
	if err != nil || !ok || oldGot.Active {
		t.Fatal("old should be retained")
	}
	if err := s.ClearRotation(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Rotation(); err != nil || ok {
		t.Fatal("journal should be cleared")
	}
}
