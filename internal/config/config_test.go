package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPathsHonorEnv(t *testing.T) {
	t.Setenv("SYNCSH_CONFIG_DIR", "/tmp/syncsh-cfg")
	t.Setenv("SYNCSH_DATA_DIR", "/tmp/syncsh-data")
	if got := ConfigDir(); got != "/tmp/syncsh-cfg" {
		t.Fatalf("ConfigDir = %q", got)
	}
	if got := DataDir(); got != "/tmp/syncsh-data" {
		t.Fatalf("DataDir = %q", got)
	}
	if got := ConfigPath(); got != filepath.Join("/tmp/syncsh-cfg", "config.yaml") {
		t.Fatalf("ConfigPath = %q", got)
	}
	if got := LocalPath(); got != filepath.Join("/tmp/syncsh-data", "local.yaml") {
		t.Fatalf("LocalPath = %q", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", cfgDir)
	t.Setenv("SYNCSH_DATA_DIR", dataDir)

	cfg := Default()
	cfg.DeviceID = "dev-1"
	cfg.DeviceName = "laptop"
	cfg.Sync.Directory.Path = "$HOME/remote"
	cfg.Sync.Callbacks = []string{"rclone sync myremote:/syncsh $HOME/GoogleDrive/syncsh"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	user, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(user), "device_id") || strings.Contains(string(user), "device_name") || strings.Contains(string(user), "database") {
		t.Fatalf("user config leaked machine-local fields:\n%s", user)
	}
	if strings.Contains(string(user), "rsync:") || strings.Contains(string(user), "scp:") {
		t.Fatalf("expected empty transports omitted:\n%s", user)
	}
	if !strings.Contains(string(user), "$HOME/remote") {
		t.Fatalf("path should be stored unexpanded:\n%s", user)
	}
	local, err := os.ReadFile(LocalPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(local), "dev-1") || !strings.Contains(string(local), "laptop") {
		t.Fatalf("local config missing identity:\n%s", local)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DeviceID != "dev-1" || loaded.DeviceName != "laptop" {
		t.Fatalf("unexpected loaded config: %+v", loaded)
	}
	if loaded.Version != CurrentVersion {
		t.Fatalf("version = %d", loaded.Version)
	}
	if loaded.Database.Path != DatabasePath() {
		t.Fatalf("database path = %q", loaded.Database.Path)
	}
	if loaded.Sync.Directory.Path != "$HOME/remote" {
		t.Fatalf("directory path = %q", loaded.Sync.Directory.Path)
	}
	if len(loaded.Sync.Callbacks) != 1 {
		t.Fatalf("callbacks = %#v", loaded.Sync.Callbacks)
	}
	if !strings.Contains(string(user), "callbacks:") {
		t.Fatalf("callbacks should be under sync:\n%s", user)
	}
}

func TestLoadSyncCallbacks(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", cfgDir)
	t.Setenv("SYNCSH_DATA_DIR", dataDir)
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("version: 1\nsync:\n  transport: directory\n  directory:\n    path: $HOME/GoogleDrive/syncsh\n  callbacks:\n    - rclone copy $HOME/GoogleDrive/syncsh gdrive:/syncsh\n")
	if err := os.WriteFile(ConfigPath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Sync.Callbacks) != 1 || !strings.Contains(cfg.Sync.Callbacks[0], "rclone copy") {
		t.Fatalf("callbacks = %#v", cfg.Sync.Callbacks)
	}
}

func TestLoadIgnoresStaleIdentityInUserConfig(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", cfgDir)
	t.Setenv("SYNCSH_DATA_DIR", dataDir)
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := []byte("version: 1\ndevice_id: stale-id\ndevice_name: stale\ndatabase:\n  path: /tmp/old.db\nsync:\n  transport: directory\n  directory:\n    path: /tmp/remote\n")
	if err := os.WriteFile(ConfigPath(), stale, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LocalPath(), []byte("device_id: real-id\ndevice_name: real\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeviceID != "real-id" || cfg.DeviceName != "real" {
		t.Fatalf("expected local identity, got %s %s", cfg.DeviceID, cfg.DeviceName)
	}
	if cfg.Database.Path != DatabasePath() {
		t.Fatalf("database path = %q", cfg.Database.Path)
	}
}

func TestExpand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SYNCSH_TEST_VAR", "xyz")
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"~", home},
		{"~/GoogleDrive/syncsh", filepath.Join(home, "GoogleDrive/syncsh")},
		{"$HOME/GoogleDrive/syncsh", filepath.Join(home, "GoogleDrive/syncsh")},
		{"rclone sync r:/x $HOME/GoogleDrive/syncsh", "rclone sync r:/x " + filepath.Join(home, "GoogleDrive/syncsh")},
		{"rclone sync r:/x ~/GoogleDrive/syncsh", "rclone sync r:/x " + filepath.Join(home, "GoogleDrive/syncsh")},
		{"prefix $SYNCSH_TEST_VAR", "prefix xyz"},
	}
	for _, tc := range cases {
		if got := Expand(tc.in); got != tc.want {
			t.Fatalf("Expand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestIntervalDuration(t *testing.T) {
	if Default().Sync.IntervalDuration() != time.Minute {
		t.Fatal("default interval")
	}
	if (Sync{Interval: "30s"}).IntervalDuration() != 30*time.Second {
		t.Fatal("parsed interval")
	}
	if (Sync{Interval: "bogus"}).IntervalDuration() != time.Minute {
		t.Fatal("invalid interval")
	}
	if Default().Sync.GCIntervalDuration() != time.Hour {
		t.Fatal("default gc interval")
	}
	if (Sync{GCInterval: "15m"}).GCIntervalDuration() != 15*time.Minute {
		t.Fatal("parsed gc interval")
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", dir)
	t.Setenv("SYNCSH_DATA_DIR", dir)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != CurrentVersion {
		t.Fatalf("version = %d", cfg.Version)
	}
	if _, err := os.Stat(ConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("expected missing config file, err=%v", err)
	}
}

func TestSuggestDefaultsAndAcceptKeys(t *testing.T) {
	if !Default().Suggest.IsEnabled() {
		t.Fatal("suggest should be on by default")
	}
	if keys := (Suggest{}).AcceptKeys(); len(keys) != 1 || keys[0] != "Right" {
		t.Fatalf("default accept: %v", keys)
	}
	off := false
	if (Suggest{Enabled: &off}).IsEnabled() {
		t.Fatal("enabled: false")
	}
	keys := (Suggest{Accept: []string{"Right", "Tab", "  ", "C-e"}}).AcceptKeys()
	if len(keys) != 3 || keys[1] != "Tab" || keys[2] != "C-e" {
		t.Fatalf("accept keys: %v", keys)
	}
}

func TestLoadSuggestAccept(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", dir)
	t.Setenv("SYNCSH_DATA_DIR", dir)
	path := ConfigPath()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("version: 1\nsuggest:\n  enabled: false\n  accept:\n    - Right\n    - Tab\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Suggest.IsEnabled() {
		t.Fatal("expected disabled")
	}
	keys := cfg.Suggest.AcceptKeys()
	if len(keys) != 2 || keys[0] != "Right" || keys[1] != "Tab" {
		t.Fatalf("accept: %v", keys)
	}
}

func TestLoadSuggestAcceptKeepsEnabledDefault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", dir)
	t.Setenv("SYNCSH_DATA_DIR", dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(), []byte("version: 1\nsuggest:\n  accept: [Tab]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Suggest.IsEnabled() {
		t.Fatal("omitted enabled should stay on")
	}
	if keys := cfg.Suggest.AcceptKeys(); len(keys) != 1 || keys[0] != "Tab" {
		t.Fatalf("accept: %v", keys)
	}
}
