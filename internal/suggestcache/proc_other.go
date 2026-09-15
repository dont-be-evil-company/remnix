//go:build !unix && !windows

package suggestcache

import (
	"os"
	"os/exec"
)

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	return err == nil && p != nil
}

func detachCmd(cmd *exec.Cmd) {}
