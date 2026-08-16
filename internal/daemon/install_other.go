//go:build !linux && !darwin && !windows

package daemon

import "fmt"

func install(string) error {
	return fmt.Errorf("daemon autostart is not supported on this OS")
}

func uninstall() error {
	return fmt.Errorf("daemon autostart is not supported on this OS")
}

func query() (InstallState, error) {
	return InstallState{Detail: "unsupported OS"}, nil
}
