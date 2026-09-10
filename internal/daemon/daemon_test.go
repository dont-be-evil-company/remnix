package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/config"
	"github.com/dont-be-evil-company/remnix/internal/crypto/keyring"
)

func TestMain(m *testing.M) {
	keyring.Use(keyring.NewMemory())
	os.Exit(m.Run())
}

func TestStatusRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REMNIX_DATA_DIR", dir)
	t.Setenv("REMNIX_CONFIG_DIR", filepath.Join(dir, "cfg"))
	writeStatus(Status{OK: true, At: time.Now().Unix(), SyncMs: 66000, GCMs: 12})
	st, err := ReadStatus()
	if err != nil || !st.OK {
		t.Fatalf("got %+v err=%v", st, err)
	}
	if st.SyncMs != 66000 || st.GCMs != 12 {
		t.Fatalf("timing fields: %+v", st)
	}
	if got := st.FormatTiming(); got != " sync=1m6s gc=12ms" {
		t.Fatalf("FormatTiming=%q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "daemon-status.json")); err != nil {
		t.Fatal(err)
	}
}

func TestFormatElapsed(t *testing.T) {
	if got := FormatElapsed(0); got != "" {
		t.Fatalf("zero: %q", got)
	}
	if got := FormatElapsed(-1); got != "" {
		t.Fatalf("neg: %q", got)
	}
	if got := FormatElapsed(12); got != "12ms" {
		t.Fatalf("12ms: %q", got)
	}
	if got := FormatElapsed(66000); got != "1m6s" {
		t.Fatalf("1m6s: %q", got)
	}
	if got := (Status{}).FormatTiming(); got != "" {
		t.Fatalf("empty status timing: %q", got)
	}
}

func TestQueryDoesNotPanic(t *testing.T) {
	if _, err := Query(); err != nil {
		t.Fatal(err)
	}
}

func TestRunOnceDisabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REMNIX_DATA_DIR", dir)
	t.Setenv("REMNIX_CONFIG_DIR", filepath.Join(dir, "cfg"))
	off := false
	cfg := config.Default()
	cfg.Sync.Enabled = &off
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if class := runOnce(context.Background()); class != "disabled" {
		t.Fatalf("class=%q", class)
	}
	st, err := ReadStatus()
	if err != nil || !st.OK || st.Class != "disabled" {
		t.Fatalf("got %+v err=%v", st, err)
	}
}

func TestRecordOK(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REMNIX_DATA_DIR", dir)
	t.Setenv("REMNIX_CONFIG_DIR", filepath.Join(dir, "cfg"))
	writeStatus(Status{OK: false, At: 1, Error: "couldn't list directory: context canceled"})
	RecordOK()
	st, err := ReadStatus()
	if err != nil || !st.OK || st.Error != "" {
		t.Fatalf("got %+v err=%v", st, err)
	}
}

func TestSchedulerSnapshot(t *testing.T) {
	sc := &SyncScheduler{}
	running, started := sc.Snapshot()
	if running || !started.IsZero() {
		t.Fatalf("idle snapshot running=%v started=%v", running, started)
	}
	sc.mu.Lock()
	sc.running = true
	sc.started = time.Now().Add(-2 * time.Second)
	sc.mu.Unlock()
	running, started = sc.Snapshot()
	if !running || started.IsZero() {
		t.Fatalf("busy snapshot running=%v started=%v", running, started)
	}
}

func TestInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !interrupted(ctx, context.Canceled) {
		t.Fatal("canceled parent ctx should be treated as shutdown")
	}
	if interrupted(context.Background(), context.Canceled) {
		t.Fatal("live parent ctx should still record a canceled remote error")
	}
}

func TestRunOnceEmptyKeyring(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REMNIX_DATA_DIR", dir)
	t.Setenv("REMNIX_CONFIG_DIR", filepath.Join(dir, "cfg"))
	cfg := config.Default()
	on := true
	cfg.Sync.Enabled = &on
	cfg.Sync.Endpoints = []config.Endpoint{config.DirectoryEndpoint("local", filepath.Join(dir, "remote"))}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	runOnce(context.Background())
	st, err := ReadStatus()
	if err != nil {
		t.Fatal(err)
	}
	if st.OK {
		t.Fatal("expected failure when keyring is empty")
	}
	if !strings.Contains(st.Error, "unlock") {
		t.Fatalf("error = %q", st.Error)
	}
}
