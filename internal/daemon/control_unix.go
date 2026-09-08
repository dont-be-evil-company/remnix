//go:build unix

package daemon

import (
	"os"
	"syscall"
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
	_ = proc.Signal(syscall.SIGTERM)
	if err := waitPing(false, controlWait); err == nil {
		return nil
	}
	_ = proc.Kill()
	return waitPing(false, 3*time.Second)
}
