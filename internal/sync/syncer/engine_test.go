package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/generations"
	"github.com/mistweaverco/syncsh/internal/crypto/keys"
	"github.com/mistweaverco/syncsh/internal/crypto/recovery"
	"github.com/mistweaverco/syncsh/internal/crypto/rotation"
	"github.com/mistweaverco/syncsh/internal/db"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/sync/gc"
	"github.com/mistweaverco/syncsh/internal/transport/directory"
)

func machine(t *testing.T, dir, deviceID, name string) (*db.DB, *Engine) {
	t.Helper()
	d, err := db.OpenAndMigrate(filepath.Join(dir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, New(d, Options{DeviceID: deviceID, DeviceName: name, Hostname: name, Transport: directory.New(filepath.Join(dir, "..", "remote"))})
}

func TestTwoDeviceSyncAndMerge(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	dbA, engA := machine(t, filepath.Join(root, "a"), "dev-a", "A")
	engA.opts.Transport = directory.New(remote)
	m, smk, encoded, err := rotation.BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := recovery.Decode(encoded)
	engA.opts.RecoverySecret = secret
	if err := engA.InitializeRemote(ctx, m, smk); err != nil {
		t.Fatal(err)
	}

	ha := history.NewStore(dbA)
	e1 := history.Entry{ID: "h1", Command: "echo a", StartTS: time.Unix(1, 0).UTC(), Cwd: "/a", DeviceID: "dev-a"}
	if _, err := ha.Insert(e1); err != nil {
		t.Fatal(err)
	}
	if err := engA.EnqueueHistoryCreated(e1); err != nil {
		t.Fatal(err)
	}
	if err := engA.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	dbB, engB := machine(t, filepath.Join(root, "b"), "dev-b", "B")
	engB.opts.Transport = directory.New(remote)
	engB.opts.RecoverySecret = secret
	if err := engB.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := history.NewStore(dbB).List(history.Filter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "echo a" {
		t.Fatalf("b missing a's history: %+v", got)
	}

	e2 := history.Entry{ID: "h2", Command: "echo b", StartTS: time.Unix(2, 0).UTC(), Cwd: "/b", DeviceID: "dev-b"}
	if _, err := history.NewStore(dbB).Insert(e2); err != nil {
		t.Fatal(err)
	}
	if err := engB.EnqueueHistoryCreated(e2); err != nil {
		t.Fatal(err)
	}
	e3 := history.Entry{ID: "h3", Command: "echo a2", StartTS: time.Unix(3, 0).UTC(), Cwd: "/a", DeviceID: "dev-a"}
	if _, err := ha.Insert(e3); err != nil {
		t.Fatal(err)
	}
	if err := engA.EnqueueHistoryCreated(e3); err != nil {
		t.Fatal(err)
	}
	if err := engA.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := engB.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := engA.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := engA.Sync(ctx); err != nil {
		t.Fatal(err)
	}

	listA, _ := ha.List(history.Filter{Limit: 10})
	listB, _ := history.NewStore(dbB).List(history.Filter{Limit: 10})
	if len(listA) != 3 || len(listB) != 3 {
		t.Fatalf("expected 3 each, got a=%d b=%d", len(listA), len(listB))
	}
}

func TestInitializeRemoteRefusesExisting(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, engA := machine(t, filepath.Join(root, "a"), "dev-a", "A")
	engA.opts.Transport = directory.New(remote)
	m, smk, _, err := rotation.BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := engA.InitializeRemote(ctx, m, smk); err != nil {
		t.Fatal(err)
	}
	_, engB := machine(t, filepath.Join(root, "b"), "dev-b", "B")
	engB.opts.Transport = directory.New(remote)
	m2, smk2, _, err := rotation.BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := engB.InitializeRemote(ctx, m2, smk2); !errors.Is(err, ErrRemoteInitialized) {
		t.Fatalf("got %v", err)
	}
}

func TestSyncIgnoresForkedGenerations(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_, eng := machine(t, filepath.Join(root, "a"), "dev-a", "A")
	eng.opts.Transport = directory.New(remote)
	m, smk, encoded, err := rotation.BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := recovery.Decode(encoded)
	eng.opts.RecoverySecret = secret
	if err := eng.InitializeRemote(ctx, m, smk); err != nil {
		t.Fatal(err)
	}
	fork := generations.Manifest{
		Version:      generations.CurrentVersion,
		GenerationID: "forked-gen",
		Seq:          1,
		Counter:      1,
		Active:       true,
	}
	body, err := json.Marshal(fork)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(remote, "keys", "generations", fork.GenerationID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remote, "keys", "generations", fork.GenerationID, "manifest"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := eng.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	got, ok, err := keys.NewStore(eng.db).Active()
	if err != nil || !ok || got.GenerationID != m.GenerationID {
		t.Fatalf("active: ok=%v err=%v %+v", ok, err, got)
	}
}

func TestNewDeviceRestoresFromCheckpoint(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dbA, engA := machine(t, filepath.Join(root, "a"), "dev-a", "A")
	engA.opts.Transport = directory.New(remote)
	m, smk, encoded, err := rotation.BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := recovery.Decode(encoded)
	engA.opts.RecoverySecret = secret
	if err := engA.InitializeRemote(ctx, m, smk); err != nil {
		t.Fatal(err)
	}
	e1 := history.Entry{ID: "h1", Command: "echo a", StartTS: time.Unix(1, 0).UTC(), DeviceID: "dev-a"}
	if _, err := history.NewStore(dbA).Insert(e1); err != nil {
		t.Fatal(err)
	}
	if err := engA.EnqueueHistoryCreated(e1); err != nil {
		t.Fatal(err)
	}
	if err := engA.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	ckpt, err := engA.CreateCheckpoint(ctx, smk, m.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := engA.PublishAck(ctx, ckpt.ID); err != nil {
		t.Fatal(err)
	}
	tr := directory.New(remote)
	plan, err := gc.Evaluate(ctx, tr, []string{"dev-a"}, ckpt.ID)
	if err != nil || !plan.Eligible {
		t.Fatalf("gc: %+v err=%v", plan, err)
	}
	if err := gc.Execute(ctx, tr, plan, false); err != nil {
		t.Fatal(err)
	}

	_, engB := machine(t, filepath.Join(root, "b"), "dev-b", "B")
	engB.opts.Transport = directory.New(remote)
	engB.opts.RecoverySecret = secret
	if err := engB.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := history.NewStore(engB.db).List(history.Filter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "echo a" {
		t.Fatalf("b missing checkpoint history: %+v", got)
	}
}

func TestRecoverGenerationFromCheckpoint(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dbA, engA := machine(t, filepath.Join(root, "a"), "dev-a", "A")
	engA.opts.Transport = directory.New(remote)
	m, smk, encoded, err := rotation.BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := recovery.Decode(encoded)
	engA.opts.RecoverySecret = secret
	if err := engA.InitializeRemote(ctx, m, smk); err != nil {
		t.Fatal(err)
	}
	e1 := history.Entry{ID: "h1", Command: "ls", StartTS: time.Unix(1, 0).UTC(), DeviceID: "dev-a"}
	if _, err := history.NewStore(dbA).Insert(e1); err != nil {
		t.Fatal(err)
	}
	if err := engA.EnqueueHistoryCreated(e1); err != nil {
		t.Fatal(err)
	}
	if err := engA.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	ckpt, err := engA.CreateCheckpoint(ctx, smk, m.GenerationID)
	if err != nil {
		t.Fatal(err)
	}
	if err := engA.PublishAck(ctx, ckpt.ID); err != nil {
		t.Fatal(err)
	}

	_, engB := machine(t, filepath.Join(root, "b"), "dev-b", "B")
	engB.opts.Transport = directory.New(remote)
	fork, _, _, err := rotation.BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := keys.NewStore(engB.db).PutGeneration(fork); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(fork)
	if err != nil {
		t.Fatal(err)
	}
	forkDir := filepath.Join(remote, "keys", "generations", fork.GenerationID)
	if err := os.MkdirAll(forkDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(forkDir, "manifest"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	fakeRemote, _ := json.Marshal(RemoteManifest{Version: CurrentVersion, Counter: 2, ActiveGeneration: fork.GenerationID, Devices: []string{"dev-b"}})
	if err := os.WriteFile(filepath.Join(remote, "metadata", "manifest"), fakeRemote, 0o600); err != nil {
		t.Fatal(err)
	}

	engB.opts.RecoverySecret = secret
	got, err := engB.RecoverGeneration(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.GenerationID != m.GenerationID {
		t.Fatalf("recovered %s want %s", got.GenerationID, m.GenerationID)
	}
	list, err := history.NewStore(engB.db).List(history.Filter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Command != "ls" {
		t.Fatalf("recovered history: %+v", list)
	}
}

func TestTombstoneConverges(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	_ = os.MkdirAll(remote, 0o700)
	ctx := context.Background()
	dbA, engA := machine(t, filepath.Join(root, "a"), "dev-a", "A")
	engA.opts.Transport = directory.New(remote)
	m, smk, encoded, err := rotation.BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := recovery.Decode(encoded)
	engA.opts.RecoverySecret = secret
	if err := engA.InitializeRemote(ctx, m, smk); err != nil {
		t.Fatal(err)
	}
	e1 := history.Entry{ID: "h1", Command: "secret", StartTS: time.Unix(1, 0).UTC(), DeviceID: "dev-a"}
	_, _ = history.NewStore(dbA).Insert(e1)
	_ = engA.EnqueueHistoryCreated(e1)
	_ = engA.Sync(ctx)

	dbB, engB := machine(t, filepath.Join(root, "b"), "dev-b", "B")
	engB.opts.Transport = directory.New(remote)
	engB.opts.RecoverySecret = secret
	_ = engB.Sync(ctx)

	gotEntry, ok, err := history.NewStore(dbA).Get("h1")
	if err != nil || !ok {
		t.Fatalf("get h1: ok=%v err=%v", ok, err)
	}
	if err := engA.EnqueueHistoryTombstoned(gotEntry); err != nil {
		t.Fatal(err)
	}
	_ = engA.Sync(ctx)
	_ = engB.Sync(ctx)
	got, _ := history.NewStore(dbB).List(history.Filter{Limit: 10})
	if len(got) != 0 {
		t.Fatalf("tombstone did not propagate: %+v", got)
	}
	_ = engB.Sync(ctx)
	got, _ = history.NewStore(dbB).List(history.Filter{Limit: 10})
	if len(got) != 0 {
		t.Fatal("resurrected")
	}
}

func TestGCBlockedWithoutAck(t *testing.T) {
	root := t.TempDir()
	tr := directory.New(root)
	plan, err := gc.Evaluate(context.Background(), tr, []string{"a", "b"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Eligible {
		t.Fatal("must not be eligible without acks")
	}
}

func TestUnlockPrefersCachedSMK(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	_, eng := machine(t, filepath.Join(root, "a"), "dev-a", "A")
	eng.opts.Transport = directory.New(remote)
	m, smk, encoded, err := rotation.BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := recovery.Decode(encoded)
	eng.opts.RecoverySecret = secret
	if err := eng.InitializeRemote(context.Background(), m, smk); err != nil {
		t.Fatal(err)
	}

	eng.opts.RecoverySecret = nil
	eng.opts.FIDO2 = []fido2.Device{}
	if _, _, err := eng.Unlock(); err == nil {
		t.Fatal("expected unlock to fail without cache, recovery, or FIDO")
	}
	eng.opts.CachedSMKs = map[string][]byte{m.GenerationID: bytes.Repeat([]byte{1}, 32)}
	if _, _, err := eng.Unlock(); err == nil {
		t.Fatal("expected unlock to fail with a wrong cached SMK")
	}

	eng.opts.CachedSMKs = map[string][]byte{m.GenerationID: smk}
	got, active, err := eng.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if active.GenerationID != m.GenerationID {
		t.Fatalf("active %s", active.GenerationID)
	}
	if !bytes.Equal(got[m.GenerationID], smk) {
		t.Fatal("cached SMK mismatch")
	}
}
