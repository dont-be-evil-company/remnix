package repository

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/crypto/generations"
	"github.com/dont-be-evil-company/remnix/internal/transport/directory"
)

func TestProbeEmpty(t *testing.T) {
	tr := directory.New(t.TempDir())
	rep, err := Probe(context.Background(), tr)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result != Empty {
		t.Fatalf("got %s (%s)", rep.Result, rep.Message)
	}
}

func TestProbeUnrelatedFile(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	if err := tr.PutAtomic(ctx, "notes.txt", bytes.NewReader([]byte("hello"))); err != nil {
		t.Fatal(err)
	}
	rep, err := Probe(ctx, tr)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result != Unrelated {
		t.Fatalf("got %s (%s)", rep.Result, rep.Message)
	}
}

func TestProbeUnrelatedNested(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	if err := tr.PutAtomic(ctx, "docs/readme.md", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	rep, err := Probe(ctx, tr)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result != Unrelated {
		t.Fatalf("got %s", rep.Result)
	}
}

func TestProbeManifestOnly(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	body, _ := json.Marshal(remoteManifest{Version: 1, ActiveGeneration: "g1"})
	if err := tr.PutAtomic(ctx, "metadata/manifest", bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	rep, err := Probe(ctx, tr)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result != Partial {
		t.Fatalf("got %s (%s)", rep.Result, rep.Message)
	}
}

func TestProbeKeysOnly(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	body, _ := json.Marshal(generations.Manifest{Version: 1, GenerationID: "g1"})
	if err := tr.PutAtomic(ctx, "keys/generations/g1/manifest", bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	rep, err := Probe(ctx, tr)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result != Partial {
		t.Fatalf("got %s (%s)", rep.Result, rep.Message)
	}
}

func TestProbeInvalidManifest(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	if err := tr.PutAtomic(ctx, "metadata/manifest", bytes.NewReader([]byte("not-json"))); err != nil {
		t.Fatal(err)
	}
	if err := tr.PutAtomic(ctx, "keys/generations/g1/manifest", bytes.NewReader([]byte(`{"generation_id":"g1","version":1}`))); err != nil {
		t.Fatal(err)
	}
	rep, err := Probe(ctx, tr)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result != Partial {
		t.Fatalf("got %s (%s)", rep.Result, rep.Message)
	}
}

func TestProbeUnsupportedVersion(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	body, _ := json.Marshal(remoteManifest{Version: 99, ActiveGeneration: "g1"})
	if err := tr.PutAtomic(ctx, "metadata/manifest", bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if err := tr.PutAtomic(ctx, "keys/generations/g1/manifest", bytes.NewReader([]byte(`{"generation_id":"g1","version":1}`))); err != nil {
		t.Fatal(err)
	}
	rep, err := Probe(ctx, tr)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result != UnsupportedVersion {
		t.Fatalf("got %s (%s)", rep.Result, rep.Message)
	}
}

func TestProbeValid(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	body, _ := json.Marshal(remoteManifest{Version: 1, ActiveGeneration: "g1"})
	if err := tr.PutAtomic(ctx, "metadata/manifest", bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if err := tr.PutAtomic(ctx, "keys/generations/g1/manifest", bytes.NewReader([]byte(`{"generation_id":"g1","version":1}`))); err != nil {
		t.Fatal(err)
	}
	rep, err := Probe(ctx, tr)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result != Valid {
		t.Fatalf("got %s (%s)", rep.Result, rep.Message)
	}
}

func TestProbeValidWithCheckpoint(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	body, _ := json.Marshal(remoteManifest{Version: 1, ActiveGeneration: "g1"})
	if err := tr.PutAtomic(ctx, "metadata/manifest", bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if err := tr.PutAtomic(ctx, "keys/generations/g1/manifest", bytes.NewReader([]byte(`{"generation_id":"g1","version":1}`))); err != nil {
		t.Fatal(err)
	}
	if err := tr.PutAtomic(ctx, "checkpoints/c1/manifest", bytes.NewReader([]byte(`{"id":"c1"}`))); err != nil {
		t.Fatal(err)
	}
	rep, err := Probe(ctx, tr)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result != Valid {
		t.Fatalf("got %s", rep.Result)
	}
}

func TestProbeEventsWithoutMetadataIsPartial(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	if err := tr.PutAtomic(ctx, "events/d1/abc.bundle", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	rep, err := Probe(ctx, tr)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Result != Partial {
		t.Fatalf("got %s (%s)", rep.Result, rep.Message)
	}
}
