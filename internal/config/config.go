package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const CurrentVersion = 1

type Config struct {
	Version            int      `yaml:"version"`
	DeviceID           string   `yaml:"-"`
	DeviceName         string   `yaml:"-"`
	Database           Database `yaml:"-"`
	Sync               Sync     `yaml:"sync"`
	Suggest            Suggest  `yaml:"suggest"`
	DisableAutoMigrate bool     `yaml:"disable_auto_migrate,omitempty"`
}

type Database struct {
	Path string
}

type localFile struct {
	DeviceID   string `yaml:"device_id"`
	DeviceName string `yaml:"device_name"`
}

type Sync struct {
	Enabled    *bool              `yaml:"enabled,omitempty"`
	Transport  string             `yaml:"transport,omitempty"`
	Interval   string             `yaml:"interval,omitempty"`
	GCInterval string             `yaml:"gc_interval,omitempty"`
	Directory  DirectoryTransport `yaml:"directory,omitempty"`
	Rsync      RsyncTransport     `yaml:"rsync,omitempty"`
	SCP        SCPTransport       `yaml:"scp,omitempty"`
	Rclone     *RcloneConfig      `yaml:"rclone,omitempty"`
	Callbacks  []string           `yaml:"callbacks,omitempty"`
}

type RcloneEngine string

const (
	RcloneEngineEmbedded RcloneEngine = "embedded"
	RcloneEngineExternal RcloneEngine = "external"
)

type RcloneConfig struct {
	Engine  RcloneEngine   `yaml:"engine,omitempty"`
	Primary string         `yaml:"primary,omitempty"`
	Remotes []RemoteConfig `yaml:"remotes,omitempty"`
}

func (c *RcloneConfig) EngineOrDefault() RcloneEngine {
	if c == nil || c.Engine == "" {
		return RcloneEngineEmbedded
	}
	return c.Engine
}

type RemoteConfig struct {
	ID           string `yaml:"id"`
	DisplayName  string `yaml:"name,omitempty"`
	RcloneRemote string `yaml:"rclone_remote"`
	Provider     string `yaml:"provider,omitempty"`
	Path         string `yaml:"path,omitempty"`
	Enabled      bool   `yaml:"enabled"`
}

func (s Sync) IsEnabled() bool {
	if s.Enabled != nil {
		return *s.Enabled
	}
	return s.Transport != "" && s.Transport != "none"
}

type DirectoryTransport struct {
	Path string `yaml:"path"`
}

type RsyncTransport struct {
	Remote string `yaml:"remote"`
	Path   string `yaml:"path"`
}

type SCPTransport struct {
	Host string `yaml:"host"`
	User string `yaml:"user"`
	Path string `yaml:"path"`
	Port int    `yaml:"port,omitempty"`
}

// Suggest is inline history completion (zsh ghost text).
type Suggest struct {
	Enabled *bool    `yaml:"enabled,omitempty"`
	Accept  []string `yaml:"accept,omitempty"`
}

func (s Suggest) IsEnabled() bool {
	if s.Enabled == nil {
		return true
	}
	return *s.Enabled
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
		Sync: Sync{
			Transport: "directory",
		},
	}
}

func Load() (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(ConfigPath())
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read config: %w", err)
		}
	} else if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Version == 0 {
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

func (c *Config) Save() error {
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
