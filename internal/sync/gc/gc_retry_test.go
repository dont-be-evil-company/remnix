package gc

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/crypto/envelope"
	"github.com/dont-be-evil-company/remnix/internal/sync/ack"
	"github.com/dont-be-evil-company/remnix/internal/sync/bundle"
	"github.com/dont-be-evil-company/remnix/internal/sync/checkpoint"
	"github.com/dont-be-evil-company/remnix/internal/sync/event"
	"github.com/dont-be-evil-company/remnix/internal/sync/merge"
	"github.com/dont-be-evil-company/remnix/internal/transport"
	"github.com/dont-be-evil-company/remnix/internal/transport/directory"
)

type gcFixture struct {
	ctx    context.Context
	tr     *directory.Transport
	oldKey string
	midKey string
	newKey string
	smk    []byte
}

func setupEligibleGC(t *testing.T) gcFixture {
	t.Helper()
	tr := directory.New(t.TempDir())
	ctx := context.Background()
	smk, _ := envelope.GenerateSMK()
	putGCBundle := func(seq int64) string {
		t.Helper()
		evs := []event.Event{{Version: 1, Type: event.TypeHistoryCreated, DeviceID: "a", Seq: seq, TimeUnix: seq, Payload: []byte("x")}}
		raw, _, err := bundle.Pack("a", "g1", evs, smk)
		if err != nil {
			t.Fatal(err)
		}
		key := path.Join("events", "a", bundle.Filename(raw))
		if err := tr.PutAtomic(ctx, key, bytes.NewReader(raw)); err != nil {
			t.Fatal(err)
		}
		return key
	}
	oldKey := putGCBundle(1)
	midKey := putGCBundle(2)
	newKey := putGCBundle(5)
	oldCkpt := checkpoint.NewManifest("old", "g1", merge.Frontier{"a": 1})
	oldCkpt.CreatedAt = 1
	newCkpt := checkpoint.NewManifest("new", "g1", merge.Frontier{"a": 3})
	newCkpt.CreatedAt = 2
	putCheckpoint(t, ctx, tr, oldCkpt)
	putCheckpoint(t, ctx, tr, newCkpt)
	body, _ := ack.Encode(ack.New("a", merge.Frontier{"a": 3}, "new"))
	if err := tr.PutAtomic(ctx, "acks/a.ack", bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	return gcFixture{ctx: ctx, tr: tr, oldKey: oldKey, midKey: midKey, newKey: newKey, smk: smk}
}

func (f gcFixture) plan(t *testing.T) Plan {
	t.Helper()
	plan, err := Evaluate(f.ctx, f.tr, []string{"a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func exists(tr transport.Transport, key string) bool {
	_, err := tr.Get(context.Background(), key)
	return err == nil
}

func TestGCRepeatedRunSucceeds(t *testing.T) {
	f := setupEligibleGC(t)
	plan := f.plan(t)
	if !plan.Eligible || len(plan.Bundles) < 2 {
		t.Fatalf("plan %+v", plan)
	}
	if err := Execute(f.ctx, f.tr, plan, false); err != nil {
		t.Fatal(err)
	}
	plan2 := f.plan(t)
	if err := Execute(f.ctx, f.tr, plan2, false); err != nil {
		t.Fatal(err)
	}
	if exists(f.tr, f.oldKey) || exists(f.tr, f.midKey) {
		t.Fatal("covered bundles should stay gone")
	}
	if !exists(f.tr, f.newKey) {
		t.Fatal("newer bundle must remain")
	}
}

func TestGCRetryAfterPartialBundleDelete(t *testing.T) {
	f := setupEligibleGC(t)
	plan := f.plan(t)
	ft := &transport.FaultTransport{Base: f.tr, FailRemovePrefix: "events/", FailRemoveAfterN: 1}
	err := Execute(f.ctx, ft, plan, false)
	if err == nil || !strings.Contains(err.Error(), "partially completed") {
		t.Fatalf("got %v", err)
	}
	gone := 0
	for _, k := range []string{f.oldKey, f.midKey} {
		if !exists(f.tr, k) {
			gone++
		}
	}
	if gone != 1 {
		t.Fatalf("expected exactly one covered bundle deleted, gone=%d", gone)
	}
	if !exists(f.tr, f.newKey) {
		t.Fatal("ineligible bundle deleted")
	}
	if err := Execute(f.ctx, f.tr, f.plan(t), false); err != nil {
		t.Fatal(err)
	}
	if exists(f.tr, f.oldKey) || exists(f.tr, f.midKey) {
		t.Fatal("retry should finish covered bundles")
	}
	if !exists(f.tr, f.newKey) {
		t.Fatal("newer bundle must remain")
	}
}

func TestGCMissingEligibleBundleIsSuccess(t *testing.T) {
	f := setupEligibleGC(t)
	plan := f.plan(t)
	if err := f.tr.Remove(f.ctx, f.oldKey); err != nil {
		t.Fatal(err)
	}
	if err := Execute(f.ctx, f.tr, plan, false); err != nil {
		t.Fatal(err)
	}
}

func TestGCFailOnFirstBundleDeleteRetries(t *testing.T) {
	f := setupEligibleGC(t)
	plan := f.plan(t)
	ft := &transport.FaultTransport{Base: f.tr, FailRemovePrefix: "events/"}
	if err := Execute(f.ctx, ft, plan, false); err == nil {
		t.Fatal("expected first bundle delete to fail")
	}
	if !exists(f.tr, f.oldKey) || !exists(f.tr, f.midKey) {
		t.Fatal("no bundles should be removed")
	}
	if err := Execute(f.ctx, f.tr, f.plan(t), false); err != nil {
		t.Fatal(err)
	}
}

func TestGCPermissionDeniedIsNotNotFound(t *testing.T) {
	f := setupEligibleGC(t)
	plan := f.plan(t)
	ft := &transport.FaultTransport{
		Base:             f.tr,
		FailRemovePrefix: "events/",
		FailRemoveErr:    transport.ErrPermissionDenied,
	}
	err := Execute(f.ctx, ft, plan, false)
	if err == nil || !errors.Is(err, transport.ErrPermissionDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestGCCancelDuringDeleteThenRetry(t *testing.T) {
	f := setupEligibleGC(t)
	plan := f.plan(t)
	ft := &transport.FaultTransport{
		Base:             f.tr,
		FailRemovePrefix: "events/",
		FailRemoveAfterN: 1,
		FailRemoveErr:    context.Canceled,
	}
	err := Execute(f.ctx, ft, plan, false)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if err := Execute(f.ctx, f.tr, f.plan(t), false); err != nil {
		t.Fatal(err)
	}
	if exists(f.tr, f.oldKey) || exists(f.tr, f.midKey) {
		t.Fatal("retry should collect remaining bundles")
	}
}

func TestGCRetryRecomputesEligibility(t *testing.T) {
	f := setupEligibleGC(t)
	plan := f.plan(t)
	ft := &transport.FaultTransport{Base: f.tr, FailRemovePrefix: "events/", FailRemoveAfterN: 1}
	if err := Execute(f.ctx, ft, plan, false); err == nil {
		t.Fatal("expected partial failure")
	}
	blocked, err := Evaluate(f.ctx, f.tr, []string{"a", "b"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Eligible {
		t.Fatal("missing ack for b should block bundle GC")
	}
	if err := Execute(f.ctx, f.tr, blocked, false); err != nil {
		t.Fatal(err)
	}
	remaining := 0
	for _, k := range []string{f.oldKey, f.midKey} {
		if exists(f.tr, k) {
			remaining++
		}
	}
	if remaining == 0 {
		t.Fatal("retry must not delete remaining bundles after eligibility shrinks")
	}
	if !exists(f.tr, f.newKey) {
		t.Fatal("newer bundle must remain")
	}
}

func TestGCDryRunAfterPartialCleanup(t *testing.T) {
	f := setupEligibleGC(t)
	plan := f.plan(t)
	ft := &transport.FaultTransport{Base: f.tr, FailRemovePrefix: "events/", FailRemoveAfterN: 1}
	if err := Execute(f.ctx, ft, plan, false); err == nil {
		t.Fatal("expected partial failure")
	}
	beforeOld, beforeMid := exists(f.tr, f.oldKey), exists(f.tr, f.midKey)
	dry := f.plan(t)
	if err := Execute(f.ctx, f.tr, dry, true); err != nil {
		t.Fatal(err)
	}
	if exists(f.tr, f.oldKey) != beforeOld || exists(f.tr, f.midKey) != beforeMid || !exists(f.tr, f.newKey) {
		t.Fatal("dry-run must not mutate")
	}
	foundRemaining := false
	for _, k := range dry.Bundles {
		if (k == f.oldKey && beforeOld) || (k == f.midKey && beforeMid) {
			foundRemaining = true
		}
		if k == f.newKey {
			t.Fatal("dry-run listed ineligible bundle")
		}
	}
	if !foundRemaining {
		t.Fatalf("dry-run should list remaining eligible bundle, got %v", dry.Bundles)
	}
}

func TestGCMultiBackendPartialFailure(t *testing.T) {
	f := setupEligibleGC(t)
	remoteB := t.TempDir()
	trB := directory.New(remoteB)
	if err := filepath.Walk(f.tr.Root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(f.tr.Root, p)
		if err != nil {
			return err
		}
		target := filepath.Join(remoteB, rel)
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
	}); err != nil {
		t.Fatal(err)
	}
	planA := f.plan(t)
	if err := Execute(f.ctx, f.tr, planA, false); err != nil {
		t.Fatal(err)
	}
	planB, err := Evaluate(f.ctx, trB, []string{"a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	ft := &transport.FaultTransport{Base: trB, FailRemovePrefix: "events/", FailRemoveAfterN: 1}
	if err := Execute(f.ctx, ft, planB, false); err == nil {
		t.Fatal("expected backend B failure")
	}
	if err := Execute(f.ctx, trB, planB, false); err != nil {
		if err := Execute(f.ctx, trB, func() Plan {
			p, err := Evaluate(f.ctx, trB, []string{"a"}, "")
			if err != nil {
				t.Fatal(err)
			}
			return p
		}(), false); err != nil {
			t.Fatal(err)
		}
	}
	if exists(trB, f.oldKey) || exists(trB, f.midKey) {
		t.Fatal("backend B should converge")
	}
	if exists(f.tr, f.oldKey) || exists(f.tr, f.midKey) {
		t.Fatal("backend A should stay clean")
	}
}
