//go:build windows

package daemon

import (
	"os"
	"time"
)

func stopProcess() error {
	if Ping() != nil {
		return nil
	}
	pid, err := readPID()
	if err != nil {
		return err
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	_ = proc.Kill()
	return waitPing(false, 3*time.Second)
}
