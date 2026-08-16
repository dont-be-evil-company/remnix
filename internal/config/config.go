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
	DeviceID           string   `yaml:"device_id"`
	DeviceName         string   `yaml:"device_name"`
	Database           Database `yaml:"database"`
	Sync               Sync     `yaml:"sync"`
	Suggest            Suggest  `yaml:"suggest"`
	DisableAutoMigrate bool     `yaml:"disable_auto_migrate,omitempty"`
}

type Database struct {
	Path string `yaml:"path"`
}

type Sync struct {
	Transport  string             `yaml:"transport"`
	Interval   string             `yaml:"interval,omitempty"`
	GCInterval string             `yaml:"gc_interval,omitempty"`
	Directory  DirectoryTransport `yaml:"directory"`
	Rsync      RsyncTransport     `yaml:"rsync"`
	SCP        SCPTransport       `yaml:"scp"`
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
	path := ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := Default()
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Version == 0 {
		cfg.Version = CurrentVersion
	}
	if cfg.Database.Path == "" {
		cfg.Database.Path = DatabasePath()
	}
	return cfg, nil
}

func (c *Config) Save() error {
	if err := os.MkdirAll(ConfigDir(), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(c.Database.Path), 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp := ConfigPath() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp, ConfigPath()); err != nil {
		return fmt.Errorf("replace config: %w", err)
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
