//go:build linux

package daemon

import "os/exec"

func restartService() error {
	return exec.Command("systemctl", "--user", "restart", "syncsh-daemon.service").Run()
}
