package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const CurrentVersion = 2

type Config struct {
	Version            int      `yaml:"version"`
	DeviceID           string   `yaml:"-"`
	DeviceName         string   `yaml:"-"`
	Database           Database `yaml:"-"`
	Sync               Sync     `yaml:"sync"`
	Suggest            Suggest  `yaml:"suggest"`
	PtyProxy           PtyProxy `yaml:"pty_proxy"`
	UI                 UI       `yaml:"ui"`
	DisableAutoMigrate bool     `yaml:"disable_auto_migrate,omitempty"`
}

type UI struct {
	Colors Colors `yaml:"colors"`
	Icons  Icons  `yaml:"icons"`
}

type Colors struct {
	Accent   string       `yaml:"accent,omitempty"`
	Title    string       `yaml:"title,omitempty"`
	Muted    string       `yaml:"muted,omitempty"`
	Rule     string       `yaml:"rule,omitempty"`
	Badge    string       `yaml:"badge,omitempty"`
	Duration string       `yaml:"duration,omitempty"`
	Failed   string       `yaml:"failed,omitempty"`
	Time     string       `yaml:"time,omitempty"`
	Text     string       `yaml:"text,omitempty"`
	Syntax   SyntaxColors `yaml:"syntax"`
}

type SyntaxColors struct {
	Command  string `yaml:"command,omitempty"`
	Keyword  string `yaml:"keyword,omitempty"`
	Flag     string `yaml:"flag,omitempty"`
	String   string `yaml:"string,omitempty"`
	Comment  string `yaml:"comment,omitempty"`
	Operator string `yaml:"operator,omitempty"`
	Variable string `yaml:"variable,omitempty"`
	Path     string `yaml:"path,omitempty"`
	Number   string `yaml:"number,omitempty"`
	Argument string `yaml:"argument,omitempty"`
}

type Icons struct {
	Cursor               string `yaml:"cursor,omitempty"`
	SuggestionTyped      string `yaml:"suggestion_typed,omitempty"`
	SuggestionHistory    string `yaml:"suggestion_history,omitempty"`
	SuggestionCompletion string `yaml:"suggestion_completion,omitempty"`
	Separator            string `yaml:"separator,omitempty"`
	MoveUpDown           string `yaml:"move_up_down,omitempty"`
}

type Database struct {
	Path string
}

type localFile struct {
	DeviceID   string `yaml:"device_id"`
	DeviceName string `yaml:"device_name"`
}

type Sync struct {
	Enabled      *bool        `yaml:"enabled,omitempty"`
	Interval     string       `yaml:"interval,omitempty"`
	GCInterval   string       `yaml:"gc_interval,omitempty"`
	RcloneEngine RcloneEngine `yaml:"rclone_engine,omitempty"`
	Endpoints    []Endpoint   `yaml:"endpoints,omitempty"`
	Callbacks    []string     `yaml:"callbacks,omitempty"`
}

type RcloneEngine string

const (
	RcloneEngineEmbedded RcloneEngine = "embedded"
	RcloneEngineExternal RcloneEngine = "external"
)

const (
	TypeRclone    = "rclone"
	TypeDirectory = "directory"
	TypeRsync     = "rsync"
	TypeSCP       = "scp"
)

type Endpoint struct {
	ID           string `yaml:"id"`
	Type         string `yaml:"type"`
	DisplayName  string `yaml:"name,omitempty"`
	Enabled      bool   `yaml:"enabled"`
	RcloneRemote string `yaml:"rclone_remote,omitempty"`
	Provider     string `yaml:"provider,omitempty"`
	Path         string `yaml:"path,omitempty"`
	Remote       string `yaml:"remote,omitempty"`
	Host         string `yaml:"host,omitempty"`
	User         string `yaml:"user,omitempty"`
	Port         int    `yaml:"port,omitempty"`
}

func (s Sync) RcloneEngineOrDefault() RcloneEngine {
	if s.RcloneEngine == "" {
		return RcloneEngineEmbedded
	}
	return s.RcloneEngine
}

func (s Sync) IsEnabled() bool {
	if s.Enabled != nil && !*s.Enabled {
		return false
	}
	return len(s.EnabledEndpoints()) > 0
}

func (s Sync) EnabledEndpoints() []Endpoint {
	var out []Endpoint
	for _, ep := range s.Endpoints {
		if ep.Enabled {
			out = append(out, ep)
		}
	}
	return out
}

func (s Sync) Endpoint(id string) (Endpoint, bool) {
	for _, ep := range s.Endpoints {
		if ep.ID == id {
			return ep, true
		}
	}
	return Endpoint{}, false
}

func (s *Sync) UpsertEndpoint(ep Endpoint) {
	for i, existing := range s.Endpoints {
		if existing.ID == ep.ID {
			s.Endpoints[i] = ep
			return
		}
	}
	s.Endpoints = append(s.Endpoints, ep)
}

func (s *Sync) RemoveEndpoint(id string) (Endpoint, bool) {
	kept := s.Endpoints[:0]
	var removed Endpoint
	found := false
	for _, ep := range s.Endpoints {
		if ep.ID == id {
			removed = ep
			found = true
			continue
		}
		kept = append(kept, ep)
	}
	s.Endpoints = kept
	return removed, found
}

func DirectoryEndpoint(id, path string) Endpoint {
	return Endpoint{ID: id, Type: TypeDirectory, Path: path, Enabled: true}
}

// SuggestIcons labels rows in the zsh LSP-style menu.
type SuggestIcons struct {
	Typed      string `yaml:"typed,omitempty"`
	History    string `yaml:"history,omitempty"`
	Completion string `yaml:"completion,omitempty"`
}

// PtyProxy wraps the interactive shell in a terminal proxy so widget TUIs can
// snapshot the live screen and draw a height-limited panel over it.
type PtyProxy struct {
	Enabled *bool          `yaml:"enabled,omitempty"`
	Height  OverlayPercent `yaml:"height,omitempty"`
}

func (p PtyProxy) IsEnabled() bool {
	return p.Enabled != nil && *p.Enabled
}

// HeightPercent is the Ctrl+R overlay height as 1-100 of the terminal.
// Unset, 0, or 100 means the full screen (same as the alt-screen widget).
func (p PtyProxy) HeightPercent() int {
	return p.Height.Percent()
}

// OverlayPercent is a 1-100 terminal-height fraction. Zero means full height.
type OverlayPercent int

func (p OverlayPercent) Percent() int {
	if p <= 0 || p >= 100 {
		return 100
	}
	return int(p)
}

func (p OverlayPercent) Full() bool {
	return p.Percent() >= 100
}

func (p *OverlayPercent) UnmarshalYAML(n *yaml.Node) error {
	if n == nil || n.Kind == 0 || n.Tag == "!!null" {
		*p = 0
		return nil
	}
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("pty_proxy.height: want a percent (for example 40 or 40%%)")
	}
	s := strings.TrimSpace(n.Value)
	if s == "" || s == "~" || s == "null" {
		*p = 0
		return nil
	}
	s = strings.TrimSpace(strings.TrimSuffix(s, "%"))
	if s == "" {
		*p = 0
		return nil
	}
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return fmt.Errorf("pty_proxy.height: invalid percent %q", n.Value)
	}
	if f > 0 && f < 1 {
		f *= 100
	}
	v := int(f + 0.5)
	if v < 0 {
		v = 0
	}
	if v > 100 {
		v = 100
	}
	*p = OverlayPercent(v)
	return nil
}

// Suggest is inline history completion (zsh ghost text).
type Suggest struct {
	Enabled     *bool        `yaml:"enabled,omitempty"`
	Menu        *bool        `yaml:"menu,omitempty"`
	MenuMax     int          `yaml:"menu_max,omitempty"`
	Completions *bool        `yaml:"completions,omitempty"`
	Icons       SuggestIcons `yaml:"icons,omitempty"`
	Accept      []string     `yaml:"accept,omitempty"`
}

func (s Suggest) IsEnabled() bool {
	if s.Enabled == nil {
		return true
	}
	return *s.Enabled
}

func (s Suggest) MenuEnabled() bool {
	return s.Menu != nil && *s.Menu
}

func (s Suggest) CompletionsEnabled() bool {
	return s.Completions != nil && *s.Completions
}

func (s Suggest) IconTyped() string {
	if strings.TrimSpace(s.Icons.Typed) == "" {
		return "›"
	}
	return s.Icons.Typed
}

func (s Suggest) IconHistory() string {
	if strings.TrimSpace(s.Icons.History) == "" {
		return "*"
	}
	return s.Icons.History
}

func (s Suggest) IconCompletion() string {
	if strings.TrimSpace(s.Icons.Completion) == "" {
		return "+"
	}
	return s.Icons.Completion
}

func (c *Config) IconTyped() string {
	if c != nil && strings.TrimSpace(c.UI.Icons.SuggestionTyped) != "" {
		return c.UI.Icons.SuggestionTyped
	}
	if c == nil {
		return "›"
	}
	return c.Suggest.IconTyped()
}

func (c *Config) IconHistory() string {
	if c != nil && strings.TrimSpace(c.UI.Icons.SuggestionHistory) != "" {
		return c.UI.Icons.SuggestionHistory
	}
	if c == nil {
		return "*"
	}
	return c.Suggest.IconHistory()
}

func (c *Config) IconCompletion() string {
	if c != nil && strings.TrimSpace(c.UI.Icons.SuggestionCompletion) != "" {
		return c.UI.Icons.SuggestionCompletion
	}
	if c == nil {
		return "+"
	}
	return c.Suggest.IconCompletion()
}

func (i Icons) CursorOrDefault() string {
	if strings.TrimSpace(i.Cursor) == "" {
		return "❯"
	}
	return i.Cursor
}

func (i Icons) SeparatorOrDefault() string {
	if strings.TrimSpace(i.Separator) == "" {
		return "·"
	}
	return i.Separator
}

func (i Icons) MoveUpDownOrDefault() string {
	if strings.TrimSpace(i.MoveUpDown) == "" {
		return "↑↓"
	}
	return i.MoveUpDown
}

func colorOr(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "default") {
		return fallback
	}
	return v
}

func (c Colors) AccentOrDefault() string   { return colorOr(c.Accent, "#F5C2E7") }
func (c Colors) TitleOrDefault() string    { return colorOr(c.Title, "#CBA6F7") }
func (c Colors) MutedOrDefault() string    { return colorOr(c.Muted, "#585B70") }
func (c Colors) RuleOrDefault() string     { return colorOr(c.Rule, "#313244") }
func (c Colors) BadgeOrDefault() string    { return colorOr(c.Badge, "#89B4FA") }
func (c Colors) DurationOrDefault() string { return colorOr(c.Duration, "#A6E3A1") }
func (c Colors) FailedOrDefault() string   { return colorOr(c.Failed, "#F38BA8") }
func (c Colors) TimeOrDefault() string     { return colorOr(c.Time, "#7F849C") }
func (c Colors) TextOrDefault() string     { return colorOr(c.Text, "#CDD6F4") }

func (s Suggest) MenuLimit() int {
	if s.MenuMax <= 0 {
		return 8
	}
	if s.MenuMax > 32 {
		return 32
	}
	return s.MenuMax
}

func (s Suggest) AcceptKeys() []string {
	if len(s.Accept) == 0 {
		return []string{"Right"}
	}
	out := make([]string, 0, len(s.Accept))
	for _, k := range s.Accept {
		k = strings.TrimSpace(k)
		if k != "" {
			out = append(out, k)
		}
	}
	if len(out) == 0 {
		return []string{"Right"}
	}
	return out
}

func Default() *Config {
	return &Config{
		Version: CurrentVersion,
		Database: Database{
			Path: DatabasePath(),
		},
		Sync: Sync{},
	}
}

func Load() (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config: %w", err)
		}
	} else {
		if err := rejectLegacyConfig(data); err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
		if err := cfg.validate(); err != nil {
			return nil, err
		}
	}
	if cfg.Version < CurrentVersion {
		cfg.Version = CurrentVersion
	}
	cfg.Database.Path = DatabasePath()
	_ = MigrateRcloneConfig()
	if err := loadLocal(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func loadLocal(cfg *Config) error {
	data, err := os.ReadFile(LocalPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read local config: %w", err)
	}
	var loc localFile
	if err := yaml.Unmarshal(data, &loc); err != nil {
		return fmt.Errorf("parse local config: %w", err)
	}
	cfg.DeviceID = loc.DeviceID
	cfg.DeviceName = loc.DeviceName
	return nil
}

func rejectLegacyConfig(data []byte) error {
	var probe struct {
		Sync struct {
			Transport string    `yaml:"transport"`
			Directory yaml.Node `yaml:"directory"`
			Rsync     yaml.Node `yaml:"rsync"`
			SCP       yaml.Node `yaml:"scp"`
			Rclone    yaml.Node `yaml:"rclone"`
		} `yaml:"sync"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return nil
	}
	legacy := probe.Sync.Transport != "" ||
		nodePresent(probe.Sync.Directory) ||
		nodePresent(probe.Sync.Rsync) ||
		nodePresent(probe.Sync.SCP) ||
		nodePresent(probe.Sync.Rclone)
	if !legacy {
		return nil
	}
	return fmt.Errorf("config format is no longer supported; run remnix setup")
}

func nodePresent(n yaml.Node) bool {
	return n.Kind != 0 && n.Tag != "!!null"
}

func validateColor(key, v string) error {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, "default") {
		return nil
	}
	if strings.HasPrefix(v, "#") {
		h := v[1:]
		if len(h) != 3 && len(h) != 6 {
			return fmt.Errorf("%s: invalid color %q (want #RGB or #RRGGBB)", key, v)
		}
		for _, r := range h {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
				return fmt.Errorf("%s: invalid color %q", key, v)
			}
		}
		return nil
	}
	return fmt.Errorf("%s: invalid color %q (want #RRGGBB, #RGB, or default)", key, v)
}

func (c *Config) validate() error {
	for _, pair := range []struct{ k, v string }{
		{"ui.colors.accent", c.UI.Colors.Accent},
		{"ui.colors.title", c.UI.Colors.Title},
		{"ui.colors.muted", c.UI.Colors.Muted},
		{"ui.colors.rule", c.UI.Colors.Rule},
		{"ui.colors.badge", c.UI.Colors.Badge},
		{"ui.colors.duration", c.UI.Colors.Duration},
		{"ui.colors.failed", c.UI.Colors.Failed},
		{"ui.colors.time", c.UI.Colors.Time},
		{"ui.colors.text", c.UI.Colors.Text},
		{"ui.colors.syntax.command", c.UI.Colors.Syntax.Command},
		{"ui.colors.syntax.keyword", c.UI.Colors.Syntax.Keyword},
		{"ui.colors.syntax.flag", c.UI.Colors.Syntax.Flag},
		{"ui.colors.syntax.string", c.UI.Colors.Syntax.String},
		{"ui.colors.syntax.comment", c.UI.Colors.Syntax.Comment},
		{"ui.colors.syntax.operator", c.UI.Colors.Syntax.Operator},
		{"ui.colors.syntax.variable", c.UI.Colors.Syntax.Variable},
		{"ui.colors.syntax.path", c.UI.Colors.Syntax.Path},
		{"ui.colors.syntax.number", c.UI.Colors.Syntax.Number},
		{"ui.colors.syntax.argument", c.UI.Colors.Syntax.Argument},
	} {
		if err := validateColor(pair.k, pair.v); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for _, ep := range c.Sync.Endpoints {
		if strings.TrimSpace(ep.ID) == "" {
			return fmt.Errorf("endpoint id is required")
		}
		if seen[ep.ID] {
			return fmt.Errorf("duplicate endpoint id %q", ep.ID)
		}
		seen[ep.ID] = true
		switch ep.Type {
		case TypeRclone, TypeDirectory, TypeRsync, TypeSCP:
		case "":
			return fmt.Errorf("endpoint %s: type is required", ep.ID)
		default:
			return fmt.Errorf("endpoint %s: unknown type %q", ep.ID, ep.Type)
		}
	}
	return nil
}

func (c *Config) Save() error {
	if err := c.validate(); err != nil {
		return err
	}
	c.Version = CurrentVersion
	if err := os.MkdirAll(ConfigDir(), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(DataDir(), 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := writeAtomic(ConfigPath(), data); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	local, err := yaml.Marshal(localFile{DeviceID: c.DeviceID, DeviceName: c.DeviceName})
	if err != nil {
		return fmt.Errorf("marshal local config: %w", err)
	}
	if err := writeAtomic(LocalPath(), local); err != nil {
		return fmt.Errorf("write local config: %w", err)
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}

func (s Sync) IntervalDuration() time.Duration {
	if s.Interval == "" {
		return time.Minute
	}
	d, err := time.ParseDuration(s.Interval)
	if err != nil || d < time.Second {
		return time.Minute
	}
	return d
}

func (s Sync) GCIntervalDuration() time.Duration {
	if s.GCInterval == "" {
		return time.Hour
	}
	d, err := time.ParseDuration(s.GCInterval)
	if err != nil || d <= 0 {
		return time.Hour
	}
	return d
}

func (c *Config) EnsureDevice(id, name string) {
	if c.DeviceID == "" {
		c.DeviceID = id
	}
	if c.DeviceName == "" {
		c.DeviceName = name
	}
}
