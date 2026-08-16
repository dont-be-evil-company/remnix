package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
