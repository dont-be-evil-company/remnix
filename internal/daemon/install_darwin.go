//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", "sh.syncsh.daemon.plist"), nil
}

func install(bin string) error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>sh.syncsh.daemon</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>daemon</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
</dict>
</plist>
`, bin)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", path).Run()
	if err := exec.Command("launchctl", "load", path).Run(); err != nil {
		return fmt.Errorf("wrote %s but launchctl load failed: %w", path, err)
	}
	return nil
}

func uninstall() error {
	path, err := plistPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", path).Run()
	_ = os.Remove(path)
	return nil
}

func query() (InstallState, error) {
	path, err := plistPath()
	if err != nil {
		return InstallState{}, err
	}
	st := InstallState{Path: path}
	if _, err := os.Stat(path); err == nil {
		st.Installed = true
		st.Detail = "LaunchAgent installed"
	}
	out, err := exec.Command("launchctl", "list", "sh.syncsh.daemon").Output()
	if err == nil && len(out) > 0 {
		st.Running = true
		st.Detail = "LaunchAgent loaded"
	}
	return st, nil
}
