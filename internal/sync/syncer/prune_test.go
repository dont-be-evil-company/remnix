package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/crypto/recovery"
	"github.com/dont-be-evil-company/remnix/internal/crypto/rotation"
	"github.com/dont-be-evil-company/remnix/internal/device"
	"github.com/dont-be-evil-company/remnix/internal/history"
	"github.com/dont-be-evil-company/remnix/internal/transport"
	"github.com/dont-be-evil-company/remnix/internal/transport/directory"
)

type pruneFixture struct {
	ctx    context.Context
	remote string
	engA   *Engine
	smk    []byte
	base   *directory.Transport
}

func setupPrunePair(t *testing.T) pruneFixture {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dbA, engA := machine(t, filepath.Join(root, "a"), "dev-a", "A")
	base := directory.New(remote)
	engA.opts.Transport = base
	m, smk, encoded, err := rotation.BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := recovery.Decode(encoded)
	engA.opts.RecoverySecret = secret
	if err := engA.InitializeRemote(ctx, m, smk); err != nil {
		t.Fatal(err)
	}

	dbB, engB := machine(t, filepath.Join(root, "b"), "dev-b", "B")
	engB.opts.Transport = directory.New(remote)
	engB.opts.RecoverySecret = secret
	e1 := history.Entry{ID: "h-b", Command: "echo from-b", StartTS: time.Unix(1, 0).UTC(), DeviceID: "dev-b"}
	if _, err := history.NewStore(dbB).Insert(e1); err != nil {
		t.Fatal(err)
	}
	if err := engB.EnqueueHistoryCreated(e1); err != nil {
		t.Fatal(err)
	}
	if err := engB.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	if err := engA.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	_ = dbA
	return pruneFixture{ctx: ctx, remote: remote, engA: engA, smk: smk, base: base}
}

func (f pruneFixture) rosterHas(t *testing.T, id string) bool {
	t.Helper()
	_, ok, err := device.NewStore(f.engA.db).Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func (f pruneFixture) manifestHasDevice(t *testing.T, id string) bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.remote, "metadata", "manifest"))
	if err != nil {
		t.Fatal(err)
	}
	var rm RemoteManifest
	if err := json.Unmarshal(raw, &rm); err != nil {
		t.Fatal(err)
	}
	if err := VerifyRemote(rm, f.smk); err != nil {
		t.Fatal(err)
	}
	for _, got := range rm.Devices {
		if got == id {
			return true
		}
	}
	return false
}

func (f pruneFixture) assertPruned(t *testing.T) {
	t.Helper()
	if f.rosterHas(t, "dev-b") {
		t.Fatal("local roster still has pruned device")
	}
	if f.manifestHasDevice(t, "dev-b") {
		t.Fatal("manifest still lists pruned device")
	}
	if _, err := os.Stat(filepath.Join(f.remote, "metadata", "devices", "dev-b.json")); !os.IsNotExist(err) {
		t.Fatalf("device metadata still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.remote, "acks", "dev-b.ack")); !os.IsNotExist(err) {
		t.Fatalf("ack still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.remote, "events", "dev-b")); !os.IsNotExist(err) {
		t.Fatalf("events still present: %v", err)
	}
}

func TestPruneManifestFailureKeepsLocalRoster(t *testing.T) {
	f := setupPrunePair(t)
	before, err := os.ReadFile(filepath.Join(f.remote, "metadata", "manifest"))
	if err != nil {
		t.Fatal(err)
	}
	ft := &transport.FaultTransport{Base: f.base, FailPutKey: "metadata/manifest"}
	f.engA.opts.Transport = ft
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err == nil {
		t.Fatal("expected manifest publish failure")
	}
	if !f.rosterHas(t, "dev-b") {
		t.Fatal("local roster must remain before commit")
	}
	after, err := os.ReadFile(filepath.Join(f.remote, "metadata", "manifest"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed manifest publish must not change the remote manifest")
	}
	f.engA.opts.Transport = f.base
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err != nil {
		t.Fatal(err)
	}
	f.assertPruned(t)
}

func TestPruneRetryAfterMetadataDeleteFailure(t *testing.T) {
	f := setupPrunePair(t)
	ft := &transport.FaultTransport{Base: f.base, FailRemovePrefix: path.Join("metadata", "devices", "dev-b.json")}
	f.engA.opts.Transport = ft
	err := f.engA.PruneDevice(f.ctx, "dev-b")
	if err == nil || !strings.Contains(err.Error(), "pruned from the manifest") {
		t.Fatalf("expected cleanup error, got %v", err)
	}
	if f.manifestHasDevice(t, "dev-b") {
		t.Fatal("manifest should have committed")
	}
	if !f.rosterHas(t, "dev-b") {
		t.Fatal("local row should remain until cleanup finishes")
	}
	f.engA.opts.Transport = f.base
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err != nil {
		t.Fatal(err)
	}
	f.assertPruned(t)
}

func TestPruneRetryAfterAckDeleteFailure(t *testing.T) {
	f := setupPrunePair(t)
	ft := &transport.FaultTransport{Base: f.base, FailRemovePrefix: path.Join("acks", "dev-b.ack")}
	f.engA.opts.Transport = ft
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err == nil {
		t.Fatal("expected ack cleanup failure")
	}
	f.engA.opts.Transport = f.base
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err != nil {
		t.Fatal(err)
	}
	f.assertPruned(t)
}

func TestPruneRetryAfterEventDeleteFailure(t *testing.T) {
	f := setupPrunePair(t)
	ft := &transport.FaultTransport{Base: f.base, FailRemovePrefix: path.Join("events", "dev-b")}
	f.engA.opts.Transport = ft
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err == nil {
		t.Fatal("expected event cleanup failure")
	}
	if _, err := os.Stat(filepath.Join(f.remote, "events", "dev-b")); err != nil {
		t.Fatalf("events should remain after failed delete: %v", err)
	}
	f.engA.opts.Transport = f.base
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err != nil {
		t.Fatal(err)
	}
	f.assertPruned(t)
}

func TestPruneMissingRemoteObjectsSucceeds(t *testing.T) {
	f := setupPrunePair(t)
	_ = os.Remove(filepath.Join(f.remote, "metadata", "devices", "dev-b.json"))
	_ = os.Remove(filepath.Join(f.remote, "acks", "dev-b.ack"))
	_ = os.RemoveAll(filepath.Join(f.remote, "events", "dev-b"))
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err != nil {
		t.Fatal(err)
	}
	f.assertPruned(t)
}

func TestPruneRetryWithMissingLocalRow(t *testing.T) {
	f := setupPrunePair(t)
	ft := &transport.FaultTransport{Base: f.base, FailRemovePrefix: path.Join("acks", "dev-b.ack")}
	f.engA.opts.Transport = ft
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err == nil {
		t.Fatal("expected cleanup failure")
	}
	if err := device.NewStore(f.engA.db).DeleteIfExists("dev-b"); err != nil {
		t.Fatal(err)
	}
	f.engA.opts.Transport = f.base
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err != nil {
		t.Fatal(err)
	}
	f.assertPruned(t)
}

func TestPruneIdempotentSecondRun(t *testing.T) {
	f := setupPrunePair(t)
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err != nil {
		t.Fatal(err)
	}
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err != nil {
		t.Fatal(err)
	}
	f.assertPruned(t)
}

func TestPrunePermissionDeniedRemainsError(t *testing.T) {
	f := setupPrunePair(t)
	ft := &transport.FaultTransport{
		Base:             f.base,
		FailRemovePrefix: path.Join("acks", "dev-b.ack"),
		FailRemoveErr:    transport.ErrPermissionDenied,
	}
	f.engA.opts.Transport = ft
	err := f.engA.PruneDevice(f.ctx, "dev-b")
	if err == nil || !errors.Is(err, transport.ErrPermissionDenied) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "pruned from the manifest") {
		t.Fatalf("expected post-commit wording: %v", err)
	}
}

func TestPruneMultiRemotePartialCleanup(t *testing.T) {
	f := setupPrunePair(t)
	remote2 := filepath.Join(t.TempDir(), "remote2")
	if err := copyDir(f.remote, remote2); err != nil {
		t.Fatal(err)
	}
	base2 := directory.New(remote2)
	eng2 := New(f.engA.db, f.engA.opts)
	eng2.opts.EndpointID = "r2"
	eng2.opts.Transport = &transport.FaultTransport{Base: base2, FailRemovePrefix: path.Join("acks", "dev-b.ack")}
	if err := f.engA.PruneDevice(f.ctx, "dev-b"); err != nil {
		t.Fatal(err)
	}
	if err := eng2.PruneDevice(f.ctx, "dev-b"); err == nil {
		t.Fatal("expected second remote cleanup failure")
	}
	eng2.opts.Transport = base2
	if err := eng2.PruneDevice(f.ctx, "dev-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(remote2, "acks", "dev-b.ack")); !os.IsNotExist(err) {
		t.Fatalf("second remote ack still present: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(remote2, "metadata", "manifest"))
	if err != nil {
		t.Fatal(err)
	}
	var rm RemoteManifest
	if err := json.Unmarshal(raw, &rm); err != nil {
		t.Fatal(err)
	}
	for _, id := range rm.Devices {
		if id == "dev-b" {
			t.Fatal("second remote manifest still lists device")
		}
	}
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return os.WriteFile(target, b, info.Mode())
	})
}
