package config

import (
	"os"
	"path/filepath"
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
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", dir)
	t.Setenv("SYNCSH_DATA_DIR", dir)

	cfg := Default()
	cfg.DeviceID = "dev-1"
	cfg.DeviceName = "laptop"
	cfg.Sync.Directory.Path = filepath.Join(dir, "remote")
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
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
