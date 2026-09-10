package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/protocol"
)

func TestRetrySyncNowWaitsOutBusy(t *testing.T) {
	n := 0
	err := retrySyncNow(context.Background(), func() error {
		n++
		if n < 3 {
			return errors.New(errSyncAlreadyRunning)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("calls=%d", n)
	}
}

func TestRetrySyncNowReturnsNonBusy(t *testing.T) {
	want := errors.New("another remnix process holds remnix.lock")
	err := retrySyncNow(context.Background(), func() error { return want })
	if err != want {
		t.Fatalf("got %v", err)
	}
}

func TestRetrySyncNowHonorsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	err := retrySyncNow(ctx, func() error { return errors.New(errSyncAlreadyRunning) })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestShouldLocalSyncFallback(t *testing.T) {
	busy := errors.New(errSyncAlreadyRunning)
	down := errors.New("daemon not reachable")
	lock := errors.New("another remnix process holds remnix.lock")
	cases := []struct {
		err       error
		reachable bool
		want      bool
	}{
		{nil, true, false},
		{busy, true, false},
		{busy, false, false},
		{lock, true, false},
		{down, false, true},
		{down, true, false},
	}
	for _, tc := range cases {
		if got := shouldLocalSyncFallback(tc.err, tc.reachable); got != tc.want {
			t.Fatalf("err=%v reachable=%v got=%v want=%v", tc.err, tc.reachable, got, tc.want)
		}
	}
}

func TestFormatLiveSync(t *testing.T) {
	if got := formatLiveSync(protocol.Stats{}); got != "sync=idle" {
		t.Fatalf("idle: %q", got)
	}
	got := formatLiveSync(protocol.Stats{SyncRunning: true, SyncStage: "pulling events (gdrive)", SyncElapsedMs: 12400})
	want := `sync=running stage="pulling events (gdrive)" elapsed=12.4s`
	if got != want {
		t.Fatalf("running: got %q want %q", got, want)
	}
	got = formatLiveSync(protocol.Stats{SyncRunning: true})
	if got != `sync=running stage="working"` {
		t.Fatalf("running no stage: %q", got)
	}
}

func TestIsSyncAlreadyRunning(t *testing.T) {
	if !isSyncAlreadyRunning(errors.New(errSyncAlreadyRunning)) {
		t.Fatal("exact busy")
	}
	if isSyncAlreadyRunning(fmt.Errorf("wrapped: %s", errSyncAlreadyRunning)) {
		t.Fatal("wrapped should not match")
	}
	if isSyncAlreadyRunning(nil) {
		t.Fatal("nil")
	}
}
