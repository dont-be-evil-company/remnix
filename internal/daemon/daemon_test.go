package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
)

func TestMain(m *testing.M) {
	keyring.Use(keyring.NewMemory())
	os.Exit(m.Run())
}

func TestStatusRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_DATA_DIR", dir)
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(dir, "cfg"))
	writeStatus(Status{OK: true, At: time.Now().Unix()})
	st, err := ReadStatus()
	if err != nil || !st.OK {
		t.Fatalf("got %+v err=%v", st, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "daemon-status.json")); err != nil {
		t.Fatal(err)
	}
}

func TestQueryDoesNotPanic(t *testing.T) {
	if _, err := Query(); err != nil {
		t.Fatal(err)
	}
}

func TestRunOnceDisabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_DATA_DIR", dir)
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(dir, "cfg"))
	off := false
	cfg := config.Default()
	cfg.Sync.Enabled = &off
	cfg.Sync.Transport = "none"
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
	t.Setenv("SYNCSH_DATA_DIR", dir)
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(dir, "cfg"))
	writeStatus(Status{OK: false, At: 1, Error: "couldn't list directory: context canceled"})
	RecordOK()
	st, err := ReadStatus()
	if err != nil || !st.OK || st.Error != "" {
		t.Fatalf("got %+v err=%v", st, err)
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
	t.Setenv("SYNCSH_DATA_DIR", dir)
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(dir, "cfg"))
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
