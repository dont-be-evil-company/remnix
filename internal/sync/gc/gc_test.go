package gc

import (
	"bytes"
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
	"github.com/mistweaverco/syncsh/internal/device"
	"github.com/mistweaverco/syncsh/internal/sync/ack"
	"github.com/mistweaverco/syncsh/internal/sync/bundle"
	"github.com/mistweaverco/syncsh/internal/sync/checkpoint"
	"github.com/mistweaverco/syncsh/internal/sync/event"
	"github.com/mistweaverco/syncsh/internal/sync/merge"
	"github.com/mistweaverco/syncsh/internal/transport/directory"
)

func TestGCBlockedUntilAcks(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	a := ack.New("a", merge.Frontier{"a": 1}, "ckpt")
	b, _ := ack.Encode(a)
	_ = tr.PutAtomic(ctx, "acks/a.ack", bytes.NewReader(b))
	plan, err := Evaluate(ctx, tr, []string{"a", "b"}, "ckpt")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Eligible {
		t.Fatal("should be blocked")
	}
}

func TestRetiredExcludedFromRequired(t *testing.T) {
	devs := []device.Device{
		{ID: "a", Status: device.StatusActive},
		{ID: "b", Status: device.StatusRetired},
	}
	ids := RequiredDevices(devs)
	if len(ids) != 1 || ids[0] != "a" {
		t.Fatalf("%v", ids)
	}
}

func TestGCEligibleAfterAcks(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		a := ack.New(id, merge.Frontier{"a": 3, "b": 2}, "ckpt")
		body, _ := ack.Encode(a)
		_ = tr.PutAtomic(ctx, "acks/"+id+".ack", bytes.NewReader(body))
	}
	plan, err := Evaluate(ctx, tr, []string{"a", "b"}, "ckpt")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Eligible {
		t.Fatalf("blocked by %v", plan.BlockedBy)
	}
}

func TestGCCollectsCoveredBundles(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	smk, _ := envelope.GenerateSMK()
	oldEvs := []event.Event{{Version: 1, Type: event.TypeHistoryCreated, DeviceID: "a", Seq: 1, TimeUnix: 1, Payload: []byte("a")}}
	newEvs := []event.Event{{Version: 1, Type: event.TypeHistoryCreated, DeviceID: "a", Seq: 5, TimeUnix: 5, Payload: []byte("b")}}
	oldRaw, _, err := bundle.Pack("a", "g1", oldEvs, smk)
	if err != nil {
		t.Fatal(err)
	}
	newRaw, _, err := bundle.Pack("a", "g1", newEvs, smk)
	if err != nil {
		t.Fatal(err)
	}
	oldKey := path.Join("events", "a", bundle.Filename(oldRaw))
	newKey := path.Join("events", "a", bundle.Filename(newRaw))
	if err := tr.PutAtomic(ctx, oldKey, bytes.NewReader(oldRaw)); err != nil {
		t.Fatal(err)
	}
	if err := tr.PutAtomic(ctx, newKey, bytes.NewReader(newRaw)); err != nil {
		t.Fatal(err)
	}

	oldCkpt := checkpoint.NewManifest("old", "g1", merge.Frontier{"a": 1})
	oldCkpt.CreatedAt = 1
	newCkpt := checkpoint.NewManifest("new", "g1", merge.Frontier{"a": 3})
	newCkpt.CreatedAt = 2
	putCheckpoint(t, ctx, tr, oldCkpt)
	putCheckpoint(t, ctx, tr, newCkpt)

	body, _ := ack.Encode(ack.New("a", merge.Frontier{"a": 3}, "new"))
	_ = tr.PutAtomic(ctx, "acks/a.ack", bytes.NewReader(body))

	plan, err := Evaluate(ctx, tr, []string{"a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Eligible || plan.Checkpoint != "new" {
		t.Fatalf("plan %+v", plan)
	}
	if len(plan.Bundles) != 1 || plan.Bundles[0] != oldKey {
		t.Fatalf("bundles %v want %s", plan.Bundles, oldKey)
	}
	if len(plan.Checkpoints) == 0 {
		t.Fatal("expected old checkpoint objects")
	}
	for _, k := range plan.Checkpoints {
		if !strings.Contains(k, "old") {
			t.Fatalf("unexpected checkpoint key %s", k)
		}
	}

	if err := Execute(ctx, tr, plan, false); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Get(ctx, oldKey); err == nil {
		t.Fatal("covered bundle should be removed")
	}
	if _, err := tr.Get(ctx, newKey); err != nil {
		t.Fatal("newer bundle must remain")
	}
	if _, err := os.Stat(filepath.Join(tr.Root, "checkpoints", "old")); !os.IsNotExist(err) {
		t.Fatal("old checkpoint directory should be removed")
	}
	if _, err := os.Stat(filepath.Join(tr.Root, "checkpoints", "new")); err != nil {
		t.Fatal("kept checkpoint directory must remain")
	}
}

func TestGCRemovesEmptyCheckpointDirsWhenBlocked(t *testing.T) {
	root := t.TempDir()
	tr := directory.New(root)
	ctx := context.Background()
	husk := filepath.Join(root, "checkpoints", "husk")
	if err := os.MkdirAll(husk, 0o700); err != nil {
		t.Fatal(err)
	}
	keep := checkpoint.NewManifest("keep", "g1", merge.Frontier{"a": 1})
	keep.CreatedAt = time.Now().UnixMilli()
	putCheckpoint(t, ctx, tr, keep)

	plan, err := Evaluate(ctx, tr, []string{"a", "missing"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Eligible {
		t.Fatal("should be blocked")
	}
	if len(plan.Checkpoints) != 1 || plan.Checkpoints[0] != "checkpoints/husk" {
		t.Fatalf("checkpoints %v", plan.Checkpoints)
	}
	if err := Execute(ctx, tr, plan, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(husk); !os.IsNotExist(err) {
		t.Fatal("empty checkpoint dir should be removed")
	}
	if _, err := os.Stat(filepath.Join(root, "checkpoints", "keep")); err != nil {
		t.Fatal("valid checkpoint must remain while gc is blocked")
	}
}

func TestCheckpointDue(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	due, err := CheckpointDue(ctx, tr, time.Hour)
	if err != nil || !due {
		t.Fatalf("empty remote should be due: due=%v err=%v", due, err)
	}
	m := checkpoint.NewManifest("c1", "g1", merge.Frontier{"a": 1})
	m.CreatedAt = time.Now().UnixMilli()
	putCheckpoint(t, ctx, tr, m)
	due, err = CheckpointDue(ctx, tr, time.Hour)
	if err != nil || due {
		t.Fatalf("fresh checkpoint should not be due: due=%v err=%v", due, err)
	}
}

func TestNewestCheckpointPrefersDominatingFrontier(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	stale := checkpoint.NewManifest("stale", "g1", merge.Frontier{"a": 1})
	stale.CreatedAt = 9_000
	good := checkpoint.NewManifest("good", "g1", merge.Frontier{"a": 50, "b": 20})
	good.CreatedAt = 1
	putCheckpoint(t, ctx, tr, stale)
	putCheckpoint(t, ctx, tr, good)
	got, ok, err := NewestCheckpoint(ctx, tr)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.ID != "good" {
		t.Fatalf("picked %s want good", got.ID)
	}
}

func TestGCDeletesNewerButDominatedCheckpoint(t *testing.T) {
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	good := checkpoint.NewManifest("good", "g1", merge.Frontier{"a": 10})
	good.CreatedAt = 1
	stale := checkpoint.NewManifest("stale", "g1", merge.Frontier{"a": 2})
	stale.CreatedAt = 9_000
	putCheckpoint(t, ctx, tr, good)
	putCheckpoint(t, ctx, tr, stale)

	plan, err := Evaluate(ctx, tr, []string{"a", "missing"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Eligible {
		t.Fatal("should be blocked")
	}
	if len(plan.Checkpoints) != 1 || plan.Checkpoints[0] != "checkpoints/stale" {
		t.Fatalf("checkpoints %v", plan.Checkpoints)
	}
}

func putCheckpoint(t *testing.T, ctx context.Context, tr *directory.Transport, m checkpoint.Manifest) {
	t.Helper()
	b, err := checkpoint.EncodeManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.PutAtomic(ctx, path.Join("checkpoints", m.ID, "manifest"), bytes.NewReader(b)); err != nil {
		t.Fatal(err)
	}
	if err := tr.PutAtomic(ctx, path.Join("checkpoints", m.ID, "snapshot"), bytes.NewReader([]byte("{}"))); err != nil {
		t.Fatal(err)
	}
}
