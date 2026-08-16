//go:build linux

package daemon

import "os/exec"

func restartService() error {
	return exec.Command("systemctl", "--user", "restart", "remnix-daemon.service").Run()
}
