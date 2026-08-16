package config

import (
	"os"
	"path/filepath"
)

const appName = "syncsh"

func ConfigDir() string {
	if d := os.Getenv("SYNCSH_CONFIG_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, appName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "."+appName)
	}
	return filepath.Join(home, ".config", appName)
}

func DataDir() string {
	if d := os.Getenv("SYNCSH_DATA_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, appName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "."+appName)
	}
	return filepath.Join(home, ".local", "share", appName)
}

func ConfigPath() string {
	return filepath.Join(ConfigDir(), "config.yaml")
}

func DatabasePath() string {
	return filepath.Join(DataDir(), "history.db")
}

func LockPath() string {
	return filepath.Join(DataDir(), "syncsh.lock")
}

func DaemonStatusPath() string {
	return filepath.Join(DataDir(), "daemon-status.json")
}
