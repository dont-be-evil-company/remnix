//go:build windows

package suggestcache

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// stillActive is Win32 STILL_ACTIVE (259). Not exported by x/sys/windows.
const stillActive = 259

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

func detachCmd(cmd *exec.Cmd) {}
