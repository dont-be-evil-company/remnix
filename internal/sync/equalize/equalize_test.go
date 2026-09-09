package equalize

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/transport/directory"
)

func TestEqualizeCopiesMissingObjects(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	if err := os.MkdirAll(filepath.Join(a, "metadata"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(b, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ta := directory.New(a)
	tb := directory.New(b)
	body, _ := json.Marshal(map[string]any{"counter": 3, "version": 1})
	if err := ta.PutAtomic(ctx, "metadata/manifest", bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if err := ta.PutAtomic(ctx, "acks/dev-a.ack", bytes.NewReader([]byte("ack"))); err != nil {
		t.Fatal(err)
	}
	if err := Equalize(ctx, []Named{
		{ID: "a", Transport: ta},
		{ID: "b", Transport: tb},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tb.Get(ctx, "metadata/manifest"); err != nil {
		t.Fatalf("manifest not copied: %v", err)
	}
	if _, err := tb.Get(ctx, "acks/dev-a.ack"); err != nil {
		t.Fatalf("ack not copied: %v", err)
	}
}

func TestEqualizeDoesNotOverwriteHigherManifestCounter(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	ctx := context.Background()
	ta := directory.New(a)
	tb := directory.New(b)
	low, _ := json.Marshal(map[string]any{"counter": 1})
	high, _ := json.Marshal(map[string]any{"counter": 9})
	if err := ta.PutAtomic(ctx, "metadata/manifest", bytes.NewReader(low)); err != nil {
		t.Fatal(err)
	}
	if err := tb.PutAtomic(ctx, "metadata/manifest", bytes.NewReader(high)); err != nil {
		t.Fatal(err)
	}
	if err := Equalize(ctx, []Named{
		{ID: "a", Transport: ta},
		{ID: "b", Transport: tb},
	}); err != nil {
		t.Fatal(err)
	}
	r, err := tb.Get(ctx, "metadata/manifest")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var rm struct {
		Counter int64 `json:"counter"`
	}
	if err := json.NewDecoder(r).Decode(&rm); err != nil {
		t.Fatal(err)
	}
	if rm.Counter != 9 {
		t.Fatalf("counter = %d", rm.Counter)
	}
}
