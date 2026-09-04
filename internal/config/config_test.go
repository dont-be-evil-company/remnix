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
	t.Setenv("SYNCSH_RUNTIME_DIR", "/tmp/syncsh-run")
	if got := RuntimeDir(); got != "/tmp/syncsh-run" {
		t.Fatalf("RuntimeDir = %q", got)
	}
	if got := AgentSocketPath(); got != "/tmp/syncsh-run/agent.sock" {
		t.Fatalf("AgentSocketPath = %q", got)
	}
	if got := PtyProxySocketPath(); !strings.HasPrefix(got, "/tmp/syncsh-run/pty-proxy-") {
		t.Fatalf("PtyProxySocketPath = %q", got)
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
	on := true
	cfg.Sync.Enabled = &on
	cfg.Sync.Endpoints = []Endpoint{DirectoryEndpoint("local", "$HOME/remote")}
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
	if len(loaded.Sync.Endpoints) != 1 || loaded.Sync.Endpoints[0].Path != "$HOME/remote" {
		t.Fatalf("endpoints = %+v", loaded.Sync.Endpoints)
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
	raw := []byte("version: 2\nsync:\n  enabled: true\n  endpoints:\n    - id: local\n      type: directory\n      path: $HOME/GoogleDrive/syncsh\n      enabled: true\n  callbacks:\n    - rclone copy $HOME/GoogleDrive/syncsh gdrive:/syncsh\n")
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
	stale := []byte("version: 2\ndevice_id: stale-id\ndevice_name: stale\ndatabase:\n  path: /tmp/old.db\nsync:\n  enabled: true\n  endpoints:\n    - id: local\n      type: directory\n      path: /tmp/remote\n      enabled: true\n")
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

func TestLoadRejectsLegacyTransport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", dir)
	t.Setenv("SYNCSH_DATA_DIR", dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("version: 1\nsync:\n  transport: directory\n  directory:\n    path: $HOME/GoogleDrive/syncsh\n")
	if err := os.WriteFile(ConfigPath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("expected legacy rejection, got %v", err)
	}
}

func TestLoadRejectsLegacyRclonePrimary(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", dir)
	t.Setenv("SYNCSH_DATA_DIR", dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte("version: 1\nsync:\n  transport: rclone\n  rclone:\n    primary: gdrive\n    remotes:\n      - id: gdrive\n        rclone_remote: gdrive\n        enabled: true\n")
	if err := os.WriteFile(ConfigPath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("expected legacy rejection, got %v", err)
	}
}

func TestSyncEnabledFalseDisables(t *testing.T) {
	ep := DirectoryEndpoint("local", "/tmp/x")
	off := false
	s := Sync{Enabled: &off, Endpoints: []Endpoint{ep}}
	if s.IsEnabled() {
		t.Fatal("enabled: false")
	}
	on := true
	if !(Sync{Enabled: &on, Endpoints: []Endpoint{ep}}).IsEnabled() {
		t.Fatal("enabled: true with endpoints")
	}
	if (Sync{Enabled: &on}).IsEnabled() {
		t.Fatal("enabled true but no endpoints")
	}
	if (Sync{}).IsEnabled() {
		t.Fatal("empty sync")
	}
	if !(Sync{Endpoints: []Endpoint{ep}}).IsEnabled() {
		t.Fatal("nil enabled with endpoints should be on")
	}
}

func TestRcloneConfigRoundTripOmitsSecrets(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", cfgDir)
	t.Setenv("SYNCSH_DATA_DIR", dataDir)
	cfg := Default()
	on := true
	cfg.Sync.Enabled = &on
	cfg.Sync.RcloneEngine = RcloneEngineEmbedded
	cfg.Sync.Endpoints = []Endpoint{{
		ID:           "personal-drive",
		Type:         TypeRclone,
		DisplayName:  "Google Drive",
		RcloneRemote: "syncsh-personal",
		Provider:     "google-drive",
		Path:         "syncsh",
		Enabled:      true,
	}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, leak := range []string{"password", "token", "secret_access_key", "client_secret"} {
		if strings.Contains(s, leak) {
			t.Fatalf("portable config leaked %s:\n%s", leak, s)
		}
	}
	if !strings.Contains(s, "rclone_remote: syncsh-personal") {
		t.Fatalf("missing rclone remote ref:\n%s", s)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sync.Endpoints) != 1 || loaded.Sync.Endpoints[0].ID != "personal-drive" {
		t.Fatalf("endpoints = %+v", loaded.Sync.Endpoints)
	}
	if loaded.Sync.RcloneEngineOrDefault() != RcloneEngineEmbedded {
		t.Fatal("engine default")
	}
}

func TestEndpointsRoundTripMixedTypes(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", cfgDir)
	t.Setenv("SYNCSH_DATA_DIR", dataDir)
	cfg := Default()
	on := true
	cfg.Sync.Enabled = &on
	cfg.Sync.Endpoints = []Endpoint{
		{ID: "gdrive", Type: TypeRclone, RcloneRemote: "syncsh-gdrive", Provider: "google-drive", Path: "syncsh", Enabled: true},
		{ID: "s3", Type: TypeRclone, RcloneRemote: "syncsh-s3", Provider: "s3", Path: "syncsh", Enabled: false},
		DirectoryEndpoint("nas", "/mnt/nas/syncsh"),
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sync.EnabledEndpoints()) != 2 {
		t.Fatalf("enabled = %+v", loaded.Sync.EnabledEndpoints())
	}
	if _, ok := loaded.Sync.Endpoint("s3"); !ok {
		t.Fatal("missing s3")
	}
}

func TestSaveRejectsDuplicateEndpointIDs(t *testing.T) {
	cfg := Default()
	cfg.Sync.Endpoints = []Endpoint{
		DirectoryEndpoint("local", "/a"),
		DirectoryEndpoint("local", "/b"),
	}
	if err := cfg.Save(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestRcloneConfigPath(t *testing.T) {
	t.Setenv("SYNCSH_DATA_DIR", "/tmp/syncsh-data")
	if got := RcloneConfigPath(); got != "/tmp/syncsh-data/rclone.conf" {
		t.Fatalf("RcloneConfigPath = %q", got)
	}
}

func TestMigrateRcloneConfigFromPortableDir(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", cfgDir)
	t.Setenv("SYNCSH_DATA_DIR", dataDir)
	src := filepath.Join(cfgDir, "rclone.conf")
	if err := os.WriteFile(src, []byte("[gdrive]\ntype = drive\ntoken = secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateRcloneConfig(); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dataDir, "rclone.conf")
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "token = secret") {
		t.Fatalf("migrated contents: %s", b)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("legacy rclone.conf should be removed from the portable config dir")
	}
}

func TestMigrateRcloneConfigOverwritesEmptyDest(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", cfgDir)
	t.Setenv("SYNCSH_DATA_DIR", dataDir)
	src := filepath.Join(cfgDir, "rclone.conf")
	dst := filepath.Join(dataDir, "rclone.conf")
	if err := os.WriteFile(src, []byte("[gdrive]\ntype = drive\ntoken = secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MigrateRcloneConfig(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "token = secret") {
		t.Fatalf("migrated contents: %s", b)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("legacy rclone.conf should be removed from the portable config dir")
	}
}

func TestLeftoverPortableRcloneConfig(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", cfgDir)
	t.Setenv("SYNCSH_DATA_DIR", dataDir)
	if p, ok := LeftoverPortableRcloneConfig(); ok {
		t.Fatalf("unexpected leftover %s", p)
	}
	src := filepath.Join(cfgDir, "rclone.conf")
	if err := os.WriteFile(src, []byte("[gdrive]\ntoken = secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, ok := LeftoverPortableRcloneConfig()
	if !ok || p != src {
		t.Fatalf("got %q ok=%v", p, ok)
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

func TestLoadSuggestMenu(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", dir)
	t.Setenv("SYNCSH_DATA_DIR", dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Suggest.MenuEnabled() {
		t.Fatal("menu should default off")
	}
	if cfg.Suggest.MenuLimit() != 8 {
		t.Fatalf("default limit %d", cfg.Suggest.MenuLimit())
	}
	if err := os.WriteFile(ConfigPath(), []byte("version: 1\nsuggest:\n  menu: true\n  menu_max: 40\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Suggest.MenuEnabled() {
		t.Fatal("menu should be on")
	}
	if cfg.Suggest.MenuLimit() != 32 {
		t.Fatalf("cap 32, got %d", cfg.Suggest.MenuLimit())
	}
}

func TestLoadPtyProxy(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", dir)
	t.Setenv("SYNCSH_DATA_DIR", dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PtyProxy.IsEnabled() {
		t.Fatal("pty_proxy should default off")
	}
	if err := os.WriteFile(ConfigPath(), []byte("version: 2\npty_proxy:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.PtyProxy.IsEnabled() {
		t.Fatal("pty_proxy should be on")
	}
	if cfg.PtyProxy.HeightPercent() != 100 {
		t.Fatalf("omitted height should be full, got %d", cfg.PtyProxy.HeightPercent())
	}
}

func TestLoadPtyProxyHeight(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", dir)
	t.Setenv("SYNCSH_DATA_DIR", dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		raw  string
		want int
	}{
		{"version: 2\npty_proxy:\n  enabled: true\n  height: 40\n", 40},
		{"version: 2\npty_proxy:\n  height: 40%\n", 40},
		{"version: 2\npty_proxy:\n  height: \"25%\"\n", 25},
		{"version: 2\npty_proxy:\n  height: 100\n", 100},
		{"version: 2\npty_proxy:\n  height: 0.5\n", 50},
	}
	for _, tc := range cases {
		if err := os.WriteFile(ConfigPath(), []byte(tc.raw), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load()
		if err != nil {
			t.Fatalf("%q: %v", tc.raw, err)
		}
		if got := cfg.PtyProxy.HeightPercent(); got != tc.want {
			t.Fatalf("%q: height %d want %d", tc.raw, got, tc.want)
		}
	}
}

func TestLoadSuggestCompletions(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", dir)
	t.Setenv("SYNCSH_DATA_DIR", dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Suggest.CompletionsEnabled() {
		t.Fatal("completions should default off")
	}
	if cfg.Suggest.IconTyped() != "›" || cfg.Suggest.IconHistory() != "*" || cfg.Suggest.IconCompletion() != "+" {
		t.Fatalf("default icons typed=%q history=%q completion=%q", cfg.Suggest.IconTyped(), cfg.Suggest.IconHistory(), cfg.Suggest.IconCompletion())
	}
	if err := os.WriteFile(ConfigPath(), []byte("version: 1\nsuggest:\n  menu: true\n  completions: true\n  icons:\n    typed: \">\"\n    history: H\n    completion: C\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Suggest.CompletionsEnabled() {
		t.Fatal("completions should be on")
	}
	if cfg.Suggest.IconTyped() != ">" || cfg.Suggest.IconHistory() != "H" || cfg.Suggest.IconCompletion() != "C" {
		t.Fatalf("custom icons typed=%q history=%q completion=%q", cfg.Suggest.IconTyped(), cfg.Suggest.IconHistory(), cfg.Suggest.IconCompletion())
	}
}
