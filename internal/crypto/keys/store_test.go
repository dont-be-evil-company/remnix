package keys

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/generations"
	"github.com/mistweaverco/syncsh/internal/crypto/slots"
	"github.com/mistweaverco/syncsh/internal/db"
)

func TestClearUnpublishedAllowsRetry(t *testing.T) {
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewStore(d)
	m := generations.Manifest{GenerationID: "g1", Seq: 1, Active: true, Slots: []slots.Slot{{
		ID: "s1", GenerationID: "g1", Type: slots.TypeRecovery, WrappedSMK: []byte("x"), Status: slots.StatusActive,
	}}}
	if err := s.PutGeneration(m); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearUnpublished(); err != nil {
		t.Fatal(err)
	}
	m.GenerationID = "g2"
	m.Slots[0].ID = "s2"
	if err := s.PutGeneration(m); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTrusted("remote", "g2", 1, "ckpt-1", []byte("{}")); err != nil {
		t.Fatal(err)
	}
	id, err := s.TrustedCheckpointID("remote")
	if err != nil || id != "ckpt-1" {
		t.Fatalf("trusted checkpoint: %q err=%v", id, err)
	}
	if err := s.SetTrusted("remote", "g2", 2, "", []byte("{2}")); err != nil {
		t.Fatal(err)
	}
	id, err = s.TrustedCheckpointID("remote")
	if err != nil || id != "ckpt-1" {
		t.Fatalf("empty checkpoint id should be preserved: %q err=%v", id, err)
	}
	if err := s.SetTrustedCheckpoint("remote", "ckpt-2"); err != nil {
		t.Fatal(err)
	}
	id, err = s.TrustedCheckpointID("remote")
	if err != nil || id != "ckpt-2" {
		t.Fatalf("set checkpoint: %q err=%v", id, err)
	}
	if err := s.ClearUnpublished(); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Active()
	if err != nil || !ok || got.GenerationID != "g2" {
		t.Fatalf("published generation should be kept: ok=%v err=%v %+v", ok, err, got)
	}
}

func TestPutGenerationSeqConflict(t *testing.T) {
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewStore(d)
	if err := s.PutGeneration(generations.Manifest{GenerationID: "g1", Seq: 1, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGeneration(generations.Manifest{GenerationID: "g2", Seq: 1, Active: true}); err == nil {
		t.Fatal("expected seq conflict")
	}
}

func TestPutGenerationSameIDIdempotent(t *testing.T) {
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s := NewStore(d)
	m := generations.Manifest{GenerationID: "g1", Seq: 1, Active: true}
	if err := s.PutGeneration(m); err != nil {
		t.Fatal(err)
	}
	if err := s.PutGeneration(m); err != nil {
		t.Fatal(err)
	}
}

func TestFIDO2SlotWrapUnwrap(t *testing.T) {
	smk, err := envelope.GenerateSMK()
	if err != nil {
		t.Fatal(err)
	}
	dev := fido2.NewFakeDevice("sk")
	sl, err := NewFIDO2Slot("g1", smk, dev)
	if err != nil {
		t.Fatal(err)
	}
	if sl.Type != slots.TypeFIDO2Hmac || len(sl.WrapParams) == 0 {
		t.Fatalf("slot: %+v", sl)
	}
	got, used, err := UnwrapAny([]slots.Slot{sl}, Unlock{FIDO2: []fido2.Device{dev}})
	if err != nil || used.ID != sl.ID {
		t.Fatalf("unwrap: %v %+v", err, used)
	}
	if !bytes.Equal(got, smk) {
		t.Fatal("smk mismatch")
	}
	other := fido2.NewFakeDevice("other")
	if _, _, err := UnwrapAny([]slots.Slot{sl}, Unlock{FIDO2: []fido2.Device{other}}); err == nil {
		t.Fatal("wrong device should not unwrap")
	}
}
