//go:build linux

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func unitDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user"), nil
}

func unitPath() (string, error) {
	dir, err := unitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "syncsh-daemon.service"), nil
}

func unitContents(bin string) string {
	return fmt.Sprintf(`[Unit]
Description=syncsh history sync daemon
After=default.target

[Service]
ExecStart=%s daemon
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`, bin)
}

func install(bin string) error {
	dir, err := unitDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path, err := unitPath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(unitContents(bin)), 0o644); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	if err := exec.Command("systemctl", "--user", "enable", "--now", "syncsh-daemon.service").Run(); err != nil {
		return fmt.Errorf("wrote %s but could not enable user unit (start it with: systemctl --user enable --now syncsh-daemon.service): %w", path, err)
	}
	return nil
}

func uninstall() error {
	_ = exec.Command("systemctl", "--user", "disable", "--now", "syncsh-daemon.service").Run()
	path, err := unitPath()
	if err != nil {
		return err
	}
	_ = os.Remove(path)
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return nil
}

func query() (InstallState, error) {
	path, err := unitPath()
	if err != nil {
		return InstallState{}, err
	}
	st := InstallState{Path: path}
	if _, err := os.Stat(path); err == nil {
		st.Installed = true
	}
	out, err := exec.Command("systemctl", "--user", "is-active", "syncsh-daemon.service").Output()
	if err == nil && strings.TrimSpace(string(out)) == "active" {
		st.Running = true
		st.Detail = "systemd user unit active"
	} else if st.Installed {
		st.Detail = "unit installed"
	}
	return st, nil
}
