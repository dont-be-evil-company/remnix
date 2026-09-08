package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mistweaverco/syncsh/internal/config"
)

const controlWait = 15 * time.Second

func Restart() error {
	st, err := Query()
	if err != nil {
		return err
	}
	if st.Installed && st.Running {
		if err := restartService(); err == nil {
			if err := waitPing(true, controlWait); err == nil {
				return nil
			}
		}
	}
	if err := stopProcess(); err != nil {
		return err
	}
	if err := startProcess(); err != nil {
		return err
	}
	return waitPing(true, controlWait)
}

func startProcess() error {
	if Ping() == nil {
		return nil
	}
	bin, err := Binary()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

func readPID() (int, error) {
	b, err := os.ReadFile(config.PIDFilePath())
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0, fmt.Errorf("invalid pid file: %w", err)
	}
	if pid <= 0 {
		return 0, fmt.Errorf("invalid pid %d", pid)
	}
	return pid, nil
}

func waitPing(wantReachable bool, d time.Duration) error {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		err := Ping()
		if wantReachable && err == nil {
			return nil
		}
		if !wantReachable && err != nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if wantReachable {
		return fmt.Errorf("daemon did not become reachable")
	}
	return fmt.Errorf("daemon did not stop")
}
