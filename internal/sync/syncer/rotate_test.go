package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/crypto/fido2"
	"github.com/dont-be-evil-company/remnix/internal/crypto/generations"
	"github.com/dont-be-evil-company/remnix/internal/crypto/keys"
	"github.com/dont-be-evil-company/remnix/internal/crypto/recovery"
	"github.com/dont-be-evil-company/remnix/internal/crypto/rotation"
	"github.com/dont-be-evil-company/remnix/internal/history"
	"github.com/dont-be-evil-company/remnix/internal/sync/bundle"
	"github.com/dont-be-evil-company/remnix/internal/sync/gc"
	"github.com/dont-be-evil-company/remnix/internal/transport"
	"github.com/dont-be-evil-company/remnix/internal/transport/directory"
)

type rotateFixture struct {
	ctx    context.Context
	remote string
	base   *directory.Transport
	eng    *Engine
	secret []byte
	oldSMK []byte
	old    generations.Manifest
}

func setupRotate(t *testing.T) rotateFixture {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	dbA, eng := machine(t, filepath.Join(root, "a"), "dev-a", "A")
	base := directory.New(remote)
	eng.opts.Transport = base
	m, smk, encoded, err := rotation.BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := recovery.Decode(encoded)
	eng.opts.RecoverySecret = secret
	ring := map[string][]byte{}
	eng.opts.CachedSMKs = ring
	eng.opts.StoreSMKs = func(smks map[string][]byte) {
		for k, v := range smks {
			ring[k] = v
		}
	}
	if err := eng.InitializeRemote(ctx, m, smk); err != nil {
		t.Fatal(err)
	}
	e1 := history.Entry{ID: "h1", Command: "echo old", StartTS: time.Unix(1, 0).UTC(), DeviceID: "dev-a"}
	if _, err := history.NewStore(dbA).Insert(e1); err != nil {
		t.Fatal(err)
	}
	if err := eng.EnqueueHistoryCreated(e1); err != nil {
		t.Fatal(err)
	}
	if err := eng.Sync(ctx); err != nil {
		t.Fatal(err)
	}
	return rotateFixture{ctx: ctx, remote: remote, base: base, eng: eng, secret: secret, oldSMK: smk, old: m}
}

func (f rotateFixture) remoteIDs(t *testing.T) []string {
	t.Helper()
	gens, err := f.eng.listRemoteGenerations(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, g := range gens {
		ids = append(ids, g.GenerationID)
	}
	return ids
}

func (f rotateFixture) activeRemote(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.remote, "metadata", "manifest"))
	if err != nil {
		t.Fatal(err)
	}
	var rm RemoteManifest
	if err := json.Unmarshal(raw, &rm); err != nil {
		t.Fatal(err)
	}
	return rm.ActiveGeneration
}

func (f rotateFixture) localActive(t *testing.T) generations.Manifest {
	t.Helper()
	got, ok, err := keys.NewStore(f.eng.db).Active()
	if err != nil || !ok {
		t.Fatalf("active: ok=%v err=%v", ok, err)
	}
	return got
}

func TestRotateGenerationHappyPath(t *testing.T) {
	f := setupRotate(t)
	if _, err := f.eng.RotateGeneration(f.ctx); err != nil {
		t.Fatal(err)
	}
	active := f.localActive(t)
	if active.Seq != 2 {
		t.Fatalf("seq %d", active.Seq)
	}
	if f.activeRemote(t) != active.GenerationID {
		t.Fatalf("remote %s local %s", f.activeRemote(t), active.GenerationID)
	}
	old, ok, err := keys.NewStore(f.eng.db).Generation(f.old.GenerationID)
	if err != nil || !ok || old.Active {
		t.Fatal("old generation should be retained")
	}
	if _, ok := f.eng.opts.CachedSMKs[active.GenerationID]; !ok {
		t.Fatal("new SMK should be in keyring cache")
	}
	ids := f.remoteIDs(t)
	if len(ids) != 2 {
		t.Fatalf("want two remote generations, got %v", ids)
	}

	e2 := history.Entry{ID: "h2", Command: "echo new", StartTS: time.Unix(2, 0).UTC(), DeviceID: "dev-a"}
	if _, err := history.NewStore(f.eng.db).Insert(e2); err != nil {
		t.Fatal(err)
	}
	if err := f.eng.EnqueueHistoryCreated(e2); err != nil {
		t.Fatal(err)
	}
	if err := f.eng.Sync(f.ctx); err != nil {
		t.Fatal(err)
	}
	objs, err := f.base.List(f.ctx, "events/")
	if err != nil {
		t.Fatal(err)
	}
	var sawOld, sawNew bool
	for _, o := range objs {
		raw, err := os.ReadFile(filepath.Join(f.remote, filepath.FromSlash(o.Key)))
		if err != nil {
			t.Fatal(err)
		}
		h, err := bundle.PeekHeader(raw)
		if err != nil {
			continue
		}
		switch h.GenerationID {
		case f.old.GenerationID:
			sawOld = true
			if _, _, err := bundle.Unpack(raw, f.oldSMK); err != nil {
				t.Fatalf("old bundle should still decrypt: %v", err)
			}
		case active.GenerationID:
			sawNew = true
		}
	}
	if !sawOld || !sawNew {
		t.Fatalf("expected old and new bundles, old=%v new=%v", sawOld, sawNew)
	}

	plan, err := gc.Evaluate(f.ctx, f.base, []string{"dev-a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Generations) != 0 {
		t.Fatalf("GC must not collect generations: %v", plan.Generations)
	}
	if _, err := os.Stat(filepath.Join(f.remote, "keys", "generations", f.old.GenerationID, "manifest")); err != nil {
		t.Fatal("old generation material must remain")
	}
}

func TestRotateRetryDoesNotMintSecondGeneration(t *testing.T) {
	f := setupRotate(t)
	if _, err := f.eng.RotateGeneration(f.ctx); err != nil {
		t.Fatal(err)
	}
	id := f.localActive(t).GenerationID
	if _, err := f.eng.RotateGeneration(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.localActive(t).GenerationID != id {
		t.Fatal("journal complete should not mint another generation")
	}
	if len(f.remoteIDs(t)) != 2 {
		t.Fatalf("got %v", f.remoteIDs(t))
	}
}

func TestRotateFailBeforeGenerationPublish(t *testing.T) {
	f := setupRotate(t)
	f.eng.opts.Transport = &transport.FaultTransport{Base: f.base, FailPutPrefix: "keys/generations/"}
	if _, err := f.eng.RotateGeneration(f.ctx); err == nil || !strings.Contains(err.Error(), "not committed") {
		t.Fatalf("got %v", err)
	}
	if f.localActive(t).GenerationID != f.old.GenerationID {
		t.Fatal("old generation must stay active")
	}
	f.eng.opts.Transport = f.base
	if _, err := f.eng.RotateGeneration(f.ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.remoteIDs(t)) != 2 {
		t.Fatalf("got %v", f.remoteIDs(t))
	}
	if f.localActive(t).Seq != 2 {
		t.Fatal("retry should finish one N+1")
	}
}

func TestRotateLostResponseAfterGenerationPublish(t *testing.T) {
	f := setupRotate(t)
	f.eng.opts.Transport = &transport.FaultTransport{
		Base:              f.base,
		FailPutPrefix:     "keys/generations/",
		FailPutAfterWrite: true,
	}
	if _, err := f.eng.RotateGeneration(f.ctx); err == nil {
		t.Fatal("expected lost-response error")
	}
	if f.localActive(t).GenerationID != f.old.GenerationID {
		t.Fatal("must not activate locally before manifest commit")
	}
	f.eng.opts.Transport = f.base
	if _, err := f.eng.RotateGeneration(f.ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.remoteIDs(t)) != 2 {
		t.Fatalf("got %v", f.remoteIDs(t))
	}
}

func TestRotateFailBeforeActiveManifest(t *testing.T) {
	f := setupRotate(t)
	f.eng.opts.Transport = &transport.FaultTransport{Base: f.base, FailPutKey: "metadata/manifest"}
	if _, err := f.eng.RotateGeneration(f.ctx); err == nil || !strings.Contains(err.Error(), "not committed") {
		t.Fatalf("got %v", err)
	}
	if f.activeRemote(t) != f.old.GenerationID {
		t.Fatal("remote must stay on old generation")
	}
	if f.localActive(t).GenerationID != f.old.GenerationID {
		t.Fatal("local must stay on old generation")
	}
	f.eng.opts.Transport = f.base
	if _, err := f.eng.RotateGeneration(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.localActive(t).Seq != 2 || f.activeRemote(t) != f.localActive(t).GenerationID {
		t.Fatal("retry should activate the staged generation")
	}
	if len(f.remoteIDs(t)) != 2 {
		t.Fatalf("got %v", f.remoteIDs(t))
	}
}

func TestRotateLostResponseDuringActiveManifest(t *testing.T) {
	f := setupRotate(t)
	f.eng.opts.Transport = &transport.FaultTransport{
		Base:              f.base,
		FailPutKey:        "metadata/manifest",
		FailPutAfterWrite: true,
	}
	if _, err := f.eng.RotateGeneration(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.localActive(t).Seq != 2 {
		t.Fatal("lost response after commit should still reconcile")
	}
}

func TestRotateReconcilesStaleLocalAfterRemoteCommit(t *testing.T) {
	f := setupRotate(t)
	if _, err := f.eng.RotateGeneration(f.ctx); err != nil {
		t.Fatal(err)
	}
	next := f.localActive(t)
	old := f.old
	old.Active = true
	next.Active = false
	ks := keys.NewStore(f.eng.db)
	if err := ks.PutGeneration(old); err != nil {
		t.Fatal(err)
	}
	if err := ks.PutGeneration(next); err != nil {
		t.Fatal(err)
	}
	if err := ks.ClearRotation(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.RotateGeneration(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.localActive(t).GenerationID != next.GenerationID {
		t.Fatal("should reconcile to remote N+1, not mint N+2")
	}
	if len(f.remoteIDs(t)) != 2 {
		t.Fatalf("got %v", f.remoteIDs(t))
	}
}

func TestRotateConflictingSeqFailsSafely(t *testing.T) {
	f := setupRotate(t)
	fork, _, _, err := rotation.BootstrapGeneration(2, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(fork)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.base.PutAtomic(f.ctx, path.Join("keys", "generations", fork.GenerationID, "manifest"), bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.eng.RotateGeneration(f.ctx); err == nil {
		t.Fatal("expected conflicting seq failure")
	} else if !strings.Contains(err.Error(), "recover") {
		t.Fatalf("got %v", err)
	}
	if f.localActive(t).GenerationID != f.old.GenerationID {
		t.Fatal("must not activate a conflicting generation")
	}
}

func TestRotateRestartUsesKeyring(t *testing.T) {
	f := setupRotate(t)
	dev := fido2.NewFakeDevice("test-fido")
	f.eng.opts.FIDO2 = []fido2.Device{dev}
	if _, err := f.eng.RotateGeneration(f.ctx); err != nil {
		t.Fatal(err)
	}
	next := f.localActive(t)
	eng2 := New(f.eng.db, f.eng.opts)
	eng2.opts.CachedSMKs = map[string][]byte{}
	for k, v := range f.eng.opts.CachedSMKs {
		eng2.opts.CachedSMKs[k] = v
	}
	eng2.opts.FIDO2 = nil
	smks, active, err := eng2.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if active.GenerationID != next.GenerationID {
		t.Fatalf("restart unlock %s want %s", active.GenerationID, next.GenerationID)
	}
	if _, ok := smks[next.GenerationID]; !ok {
		t.Fatal("new SMK missing after restart")
	}
}

func TestRotateFailGenerationPrefixThenRetrySameID(t *testing.T) {
	f := setupRotate(t)
	f.eng.opts.Transport = &transport.FaultTransport{Base: f.base, FailPutPrefix: "keys/generations/"}
	_, _ = f.eng.RotateGeneration(f.ctx)
	rot, ok, err := keys.NewStore(f.eng.db).Rotation()
	if err != nil || !ok {
		t.Fatalf("journal should exist: ok=%v err=%v", ok, err)
	}
	staged := rot.NewGenerationID
	f.eng.opts.Transport = f.base
	if _, err := f.eng.RotateGeneration(f.ctx); err != nil {
		t.Fatal(err)
	}
	if f.localActive(t).GenerationID != staged {
		t.Fatalf("retry used %s want staged %s", f.localActive(t).GenerationID, staged)
	}
}
