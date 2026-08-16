package config

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestMarshalUserConfigInitialWriteIncludesDefaults(t *testing.T) {
	cfg := Default()
	out, err := marshalUserConfig(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{
		"disable_auto_migrate: false",
		"enabled: false",
		"interval: 1m",
		"gc_interval: 1h",
		"rclone_engine: embedded",
		"endpoints: []",
		"callbacks: []",
		"enabled: true",
		"menu: false",
		"menu_max: 8",
		"completions: false",
		`typed: "›"`,
		`history: "*"`,
		`completion: "+"`,
		"- Right",
		"height: 100",
		`accent: "#F5C2E7"`,
		`command: "#89B4FA"`,
		`cursor: "❯"`,
		`separator: "·"`,
		`move_up_down: "↑↓"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "device_id") || strings.Contains(got, "database") {
		t.Fatalf("leaked machine-local fields:\n%s", got)
	}
}

func TestMarshalUserConfigInitialWriteKeepsUserValues(t *testing.T) {
	cfg := Default()
	on := true
	cfg.Sync.Enabled = &on
	cfg.Sync.Interval = "30s"
	cfg.Sync.Endpoints = []Endpoint{DirectoryEndpoint("local", "$HOME/remote")}
	cfg.Suggest.MenuMax = 12
	out, err := marshalUserConfig(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "enabled: true") {
		t.Fatalf("expected sync enabled:\n%s", got)
	}
	if !strings.Contains(got, "interval: 30s") {
		t.Fatalf("expected custom interval:\n%s", got)
	}
	if !strings.Contains(got, "id: local") || !strings.Contains(got, "path: $HOME/remote") {
		t.Fatalf("expected endpoint:\n%s", got)
	}
	if strings.Contains(got, "endpoints: []") {
		t.Fatalf("empty endpoints overwrote user remotes:\n%s", got)
	}
	if !strings.Contains(got, "menu_max: 12") {
		t.Fatalf("expected custom menu_max:\n%s", got)
	}
	if !strings.Contains(got, "rclone_engine: embedded") || !strings.Contains(got, "menu: false") {
		t.Fatalf("lost untouched defaults:\n%s", got)
	}
}

func TestMarshalUserConfigWritesSchemaOnce(t *testing.T) {
	cfg := Default()
	on := true
	cfg.Sync.Enabled = &on
	cfg.Sync.Endpoints = []Endpoint{DirectoryEndpoint("local", "$HOME/remote")}

	first, err := marshalUserConfig(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := string(first)
	if !strings.HasPrefix(got, schemaComment+"\n---\n") {
		t.Fatalf("missing schema header:\n%s", got)
	}
	if strings.Count(got, schemaComment) != 1 {
		t.Fatalf("schema comment count = %d:\n%s", strings.Count(got, schemaComment), got)
	}
	if strings.Count(got, "\n---\n") != 1 {
		t.Fatalf("document marker count = %d:\n%s", strings.Count(got, "\n---\n"), got)
	}

	again, err := marshalUserConfig(cfg, first)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(again), schemaComment) != 1 {
		t.Fatalf("re-save duplicated schema comment:\n%s", again)
	}
	if strings.Count(string(again), "\n---\n") != 1 {
		t.Fatalf("re-save duplicated ---:\n%s", again)
	}
}

func TestMarshalUserConfigPreservesSpaceIndent(t *testing.T) {
	cfg := Default()
	on := true
	cfg.Sync.Enabled = &on
	existing := []byte("version: 2\nsync:\n  enabled: true\n")
	out, err := marshalUserConfig(cfg, existing)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "\nsync:\n  enabled: true\n") {
		t.Fatalf("expected 2-space indent:\n%s", got)
	}
	if strings.Contains(got, "\nsync:\n    enabled:") {
		t.Fatalf("rewrote to 4-space indent:\n%s", got)
	}
}

func TestMarshalUserConfigPreservesTabIndent(t *testing.T) {
	cfg := Default()
	on := true
	cfg.Sync.Enabled = &on
	cfg.Sync.Endpoints = []Endpoint{{
		ID:      "gdrive",
		Type:    TypeRclone,
		Enabled: true,
		Path:    "remnix",
	}}
	existing := []byte("version: 2\nsync:\n\tenabled: true\n")
	out, err := marshalUserConfig(cfg, existing)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "\nsync:\n\tenabled: true\n") {
		t.Fatalf("expected tab indent:\n%s", got)
	}
	if !strings.Contains(got, "\t\t- id: gdrive\n") {
		t.Fatalf("expected nested tab indent for list:\n%s", got)
	}
	if strings.Contains(got, "\n    enabled:") || strings.Contains(got, "\n  enabled:") {
		t.Fatalf("rewrote tabs to spaces:\n%s", got)
	}
}

func TestDetectYAMLIndent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want yamlIndent
	}{
		{"empty", "", yamlIndent{spaces: 4}},
		{"two space", "version: 2\nsync:\n  enabled: true\n", yamlIndent{spaces: 2}},
		{"four space", "version: 2\nsync:\n    enabled: true\n", yamlIndent{spaces: 4}},
		{"tabs", "version: 2\nsync:\n\tenabled: true\n", yamlIndent{spaces: 4, tabs: true}},
		{"skips list alignment", "suggest:\n  accept:\n    - Right\n", yamlIndent{spaces: 2}},
		{"existing schema header", schemaComment + "\n---\nversion: 2\nsync:\n  enabled: true\n", yamlIndent{spaces: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := detectYAMLIndent([]byte(tc.in))
			if got != tc.want {
				t.Fatalf("got %+v want %+v", got, tc.want)
			}
		})
	}
}

func TestSaveWritesSchemaAndKeepsIndent(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv("REMNIX_CONFIG_DIR", cfgDir)
	t.Setenv("REMNIX_DATA_DIR", dataDir)
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := []byte(schemaComment + "\n---\nversion: 2\nsync:\n  enabled: true\n")
	if err := os.WriteFile(ConfigPath(), existing, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	on := true
	cfg.Sync.Enabled = &on
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	if strings.Count(got, schemaComment) != 1 {
		t.Fatalf("schema comment count = %d:\n%s", strings.Count(got, schemaComment), got)
	}
	if !strings.Contains(got, "\nsync:\n  enabled: true\n") {
		t.Fatalf("lost 2-space indent:\n%s", got)
	}
}

func TestMarshalUserConfigRetainsComments(t *testing.T) {
	existing := []byte(`# yaml-language-server: $schema=https://remnix.app/config.schema.json
---
# file-level note
version: 2 # format
# drive sync
sync:
    enabled: true # on
    # engine choice
    rclone_engine: embedded
    endpoints:
        # my drive
        - id: gdrive
          type: rclone # transport
          path: remnix
suggest:
    accept:
        # accept ghost
        - Right
`)
	on := true
	cfg := Default()
	cfg.Sync.Enabled = &on
	cfg.Sync.RcloneEngine = RcloneEngineEmbedded
	cfg.Sync.Endpoints = []Endpoint{{
		ID:      "gdrive",
		Type:    TypeRclone,
		Enabled: true,
		Path:    "remnix",
	}}
	cfg.Suggest.Accept = []string{"Right"}

	out, err := marshalUserConfig(cfg, existing)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if strings.Count(got, schemaComment) != 1 {
		t.Fatalf("schema comment count = %d:\n%s", strings.Count(got, schemaComment), got)
	}
	for _, want := range []string{
		"# file-level note",
		"version: 2 # format",
		"# drive sync",
		"enabled: true # on",
		"# engine choice",
		"# my drive",
		"type: rclone # transport",
		"# accept ghost",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q:\n%s", want, got)
		}
	}
}

func TestMarshalUserConfigKeepsEndpointCommentsOnUpdate(t *testing.T) {
	existing := []byte(`version: 2
sync:
    endpoints:
        # keep me
        - id: gdrive
          type: rclone
          path: remnix
`)
	cfg := Default()
	on := true
	cfg.Sync.Enabled = &on
	cfg.Sync.Endpoints = []Endpoint{{
		ID:           "gdrive",
		Type:         TypeRclone,
		Enabled:      true,
		Path:         "other",
		RcloneRemote: "gdrive",
	}}
	out, err := marshalUserConfig(cfg, existing)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "# keep me") {
		t.Fatalf("lost endpoint comment:\n%s", got)
	}
	if !strings.Contains(got, "path: other") {
		t.Fatalf("did not update path:\n%s", got)
	}
}

func TestSchemaFileIsJSON(t *testing.T) {
	data, err := os.ReadFile("../../config.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["$id"] != schemaURL {
		t.Fatalf("$id = %v", doc["$id"])
	}
}
