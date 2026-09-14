//go:build !unix

package helpparse

import "os/exec"

func isolateHelpCmd(cmd *exec.Cmd) {}
