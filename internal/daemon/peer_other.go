//go:build !linux

package daemon

import "net"

func peerAllowed(conn net.Conn) bool {
	return true
}
