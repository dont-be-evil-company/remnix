package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
	"github.com/mistweaverco/syncsh/internal/crypto/recovery"
	"github.com/mistweaverco/syncsh/internal/crypto/rotation"
	"github.com/mistweaverco/syncsh/internal/history"
)

func TestMain(m *testing.M) {
	keyring.Use(keyring.NewMemory())
	os.Exit(m.Run())
}

func TestRunCallbacksContinuesAfterFailure(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first")
	second := filepath.Join(dir, "second")
	err := runCallbacks(context.Background(), []string{
		"touch " + first,
		"false",
		"touch " + second,
		"  ",
	})
	if err == nil {
		t.Fatal("expected callback error")
	}
	if !strings.Contains(err.Error(), "callback 2") {
		t.Fatalf("error = %v", err)
	}
	if _, statErr := os.Stat(first); statErr != nil {
		t.Fatal("first callback did not run")
	}
	if _, statErr := os.Stat(second); statErr != nil {
		t.Fatal("later callback skipped after failure")
	}
}

func TestRunCallbacksIncludesCommandOutput(t *testing.T) {
	err := runCallbacks(context.Background(), []string{`echo "Safety abort: too many deletes" >&2; exit 1`})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "too many deletes") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunCallbacksRedactsSecrets(t *testing.T) {
	err := runCallbacks(context.Background(), []string{`echo "ERROR password=supersecret token=abc" >&2; exit 1`})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "supersecret") || strings.Contains(err.Error(), "abc") {
		t.Fatalf("leaked: %v", err)
	}
}

func TestRunCallbacksExpandsEnvAndTilde(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	marker := filepath.Join(home, "from-tilde")
	envMarker := filepath.Join(home, "from-env")
	t.Setenv("SYNCSH_CB_MARK", envMarker)
	if err := runCallbacks(context.Background(), []string{
		"touch ~/from-tilde",
		"touch $SYNCSH_CB_MARK",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("tilde not expanded")
	}
	if _, err := os.Stat(envMarker); err != nil {
		t.Fatal("env not expanded")
	}
}

func TestSyncRunsCallbacksAndReleasesLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(root, "cfg"))
	t.Setenv("SYNCSH_DATA_DIR", filepath.Join(root, "data"))
	a, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	remote := filepath.Join(root, "remote")
	a.Config.DeviceName = "test"
	a.Config.Sync.Transport = "directory"
	a.Config.Sync.Directory.Path = remote
	if err := a.Config.Save(); err != nil {
		t.Fatal(err)
	}
	if err := a.EnsureLocalDevice(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m, smk, encoded, err := rotation.BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := recovery.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := a.Engine(secret, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.InitializeRemote(ctx, m, smk); err != nil {
		t.Fatal(err)
	}
	if err := keyring.Set(a.Config.DeviceID, map[string][]byte{m.GenerationID: smk}); err != nil {
		t.Fatal(err)
	}

	okFile := filepath.Join(root, "ok")
	afterFail := filepath.Join(root, "after-fail")
	lockState := filepath.Join(root, "lock-state")
	lockPath := config.LockPath()
	a.Config.Sync.Callbacks = []string{
		"touch " + okFile,
		`if flock -n '` + lockPath + `' true; then echo unlocked > '` + lockState + `'; else echo locked > '` + lockState + `'; fi`,
		"false",
		"touch " + afterFail,
	}
	if err := a.Sync(ctx, nil, nil, nil); err == nil {
		t.Fatal("expected callback failure")
	} else if !strings.Contains(err.Error(), "callback 3") {
		t.Fatalf("error = %v", err)
	}
	for _, p := range []string{okFile, afterFail} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
	}
	got, err := os.ReadFile(lockState)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "locked" {
		t.Fatalf("lock during callbacks: %s", got)
	}
	l, err := AcquireLock()
	if err != nil {
		t.Fatalf("lock not released after sync: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncSkipsCallbacksWhenEngineFails(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(root, "cfg"))
	t.Setenv("SYNCSH_DATA_DIR", filepath.Join(root, "data"))
	a, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.EnsureLocalDevice(); err != nil {
		t.Fatal(err)
	}
	a.Config.Sync.Directory.Path = filepath.Join(root, "remote")
	if err := os.MkdirAll(a.Config.Sync.Directory.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "should-not-exist")
	a.Config.Sync.Callbacks = []string{"touch " + marker}
	if err := a.Sync(context.Background(), nil, nil, nil); err == nil {
		t.Fatal("expected sync error")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("callback ran after failed sync")
	}
}

func TestEnqueueHistoryCreatedSkipsRemote(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(root, "cfg"))
	t.Setenv("SYNCSH_DATA_DIR", filepath.Join(root, "data"))
	a, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.EnsureLocalDevice(); err != nil {
		t.Fatal(err)
	}
	a.Config.Sync.Transport = "rclone"
	a.Config.Sync.Rclone = &config.RcloneConfig{Primary: "missing"}
	if err := a.EnqueueHistoryCreated(history.Entry{
		ID: "h1", Command: "echo hi", DeviceID: a.Config.DeviceID,
	}); err != nil {
		t.Fatal(err)
	}
}
