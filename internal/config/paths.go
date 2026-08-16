package config

import (
	"os"
	"path/filepath"
	"strconv"
)

const appName = "remnix"

func ConfigDir() string {
	if d := os.Getenv("REMNIX_CONFIG_DIR"); d != "" {
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
	if d := os.Getenv("REMNIX_DATA_DIR"); d != "" {
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

// RcloneConfigPath is the machine-local rclone credential file. It lives next
// to local.yaml in the data directory so ~/.config/remnix/config.yaml can be
// committed without tokens.
func RcloneConfigPath() string {
	return filepath.Join(DataDir(), "rclone.conf")
}

func legacyRcloneConfigPath() string {
	return filepath.Join(ConfigDir(), "rclone.conf")
}

func rcloneConfHasContent(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

// LeftoverPortableRcloneConfig reports a secrets file still sitting in the
// commit-safe config directory after migration to the data dir.
func LeftoverPortableRcloneConfig() (string, bool) {
	src := legacyRcloneConfigPath()
	if src == RcloneConfigPath() {
		return "", false
	}
	if !rcloneConfHasContent(src) {
		return "", false
	}
	return src, true
}

// MigrateRcloneConfig moves a leftover rclone.conf out of the portable config
// directory. Safe to call repeatedly. An empty destination (created by a
// previous Init) is treated as missing so tokens are not stranded next to
// config.yaml.
func MigrateRcloneConfig() error {
	dst := RcloneConfigPath()
	src := legacyRcloneConfigPath()
	if src == dst {
		return nil
	}
	if rcloneConfHasContent(dst) {
		return nil
	}
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	if err := os.MkdirAll(DataDir(), 0o700); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		_ = os.Chmod(dst, 0o600)
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return err
	}
	return os.Remove(src)
}

func SetupStatePath() string {
	return filepath.Join(DataDir(), "setup-state.json")
}

func LocalPath() string {
	return filepath.Join(DataDir(), "local.yaml")
}

func DatabasePath() string {
	return filepath.Join(DataDir(), "history.db")
}

func LockPath() string {
	return filepath.Join(DataDir(), "remnix.lock")
}

func DaemonStatusPath() string {
	return filepath.Join(DataDir(), "daemon-status.json")
}

// RuntimeDir is where the shell-agent unix socket lives. Prefer the session
// runtime dir so the socket disappears on logout.
func RuntimeDir() string {
	if d := os.Getenv("REMNIX_RUNTIME_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, appName)
	}
	return filepath.Join(os.TempDir(), appName)
}

func AgentSocketPath() string {
	return filepath.Join(RuntimeDir(), "agent.sock")
}

func ControlSocketPath() string {
	return filepath.Join(RuntimeDir(), "control.sock")
}

func TerminalSocketPath() string {
	return filepath.Join(RuntimeDir(), "terminal.sock")
}

func PIDFilePath() string {
	return filepath.Join(RuntimeDir(), "daemon.pid")
}

// PtyProxySocketPath is the per-process screen-snapshot socket for pty-proxy.
func PtyProxySocketPath() string {
	return filepath.Join(RuntimeDir(), "pty-proxy-"+strconv.Itoa(os.Getpid())+".sock")
}
