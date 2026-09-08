//go:build darwin

package daemon

import (
	"fmt"
	"os"
	"os/exec"
)

func restartService() error {
	label := fmt.Sprintf("gui/%d/sh.syncsh.daemon", os.Getuid())
	return exec.Command("launchctl", "kickstart", "-k", label).Run()
}
