//go:build windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func startupCmd() (string, error) {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		appdata = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appdata, `Microsoft\Windows\Start Menu\Programs\Startup`, "remnix-daemon.cmd"), nil
}

func install(bin string) error {
	path, err := startupCmd()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf("@echo off\r\nstart \"\" /min \"%s\" daemon\r\n", bin)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	tr := fmt.Sprintf(`"%s" daemon`, bin)
	_ = exec.Command("schtasks", "/Create", "/TN", "remnix-daemon", "/TR", tr, "/SC", "ONLOGON", "/F").Run()
	return nil
}

func uninstall() error {
	_ = exec.Command("schtasks", "/Delete", "/TN", "remnix-daemon", "/F").Run()
	path, err := startupCmd()
	if err != nil {
		return err
	}
	_ = os.Remove(path)
	return nil
}

func query() (InstallState, error) {
	path, err := startupCmd()
	if err != nil {
		return InstallState{}, err
	}
	st := InstallState{Path: path}
	if _, err := os.Stat(path); err == nil {
		st.Installed = true
		st.Detail = "Startup folder command installed"
	}
	if err := exec.Command("schtasks", "/Query", "/TN", "remnix-daemon").Run(); err == nil {
		st.Installed = true
		st.Detail = "logon scheduled task installed"
	}
	return st, nil
}
