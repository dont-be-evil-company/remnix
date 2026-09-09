//go:build !unix

package terminal

import (
	"fmt"
	"net"

	"github.com/dont-be-evil-company/remnix/internal/ptyproxy"
)

type Session struct {
	ID   string
	Cols int
	Rows int
}

func startSession(id string, req CreateRequest, conn net.Conn, rpc RPC) (*Session, error) {
	return nil, fmt.Errorf("terminal sessions are not supported on this platform")
}

func (s *Session) Snapshot() ptyproxy.Snapshot { return ptyproxy.Snapshot{} }
func (s *Session) Wait() int                   { return 1 }
func (s *Session) Close()                      {}
func (s *Session) BeginOverlay(rowsFor func(int) int) (*Overlay, error) {
	_ = rowsFor
	return nil, fmt.Errorf("terminal sessions are not supported on this platform")
}
func (s *Session) sendFrame(kind byte, payload []byte) error {
	_ = kind
	_ = payload
	return fmt.Errorf("terminal sessions are not supported on this platform")
}
