//go:build linux

package daemon

import (
	"net"
	"os"
	"syscall"
)

func peerAllowed(conn net.Conn) bool {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return true
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return false
	}
	var uid uint32
	var sysErr error
	if err := raw.Control(func(fd uintptr) {
		cred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if err != nil {
			sysErr = err
			return
		}
		uid = cred.Uid
	}); err != nil || sysErr != nil {
		return false
	}
	return uid == uint32(os.Getuid())
}
