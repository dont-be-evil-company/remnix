package terminal

import (
	"fmt"
	"net"
	"sync"

	"github.com/google/uuid"
	"github.com/mistweaverco/syncsh/internal/protocol"
)

type RPC struct {
	Start   func(command, cwd, session, shell string) (string, error)
	End     func(id string, exit int) error
	Suggest func(prefix, cwd string) (string, error)
}

type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	rpc      RPC
}

func NewManager() *Manager {
	return NewManagerRPC(RPC{})
}

func NewManagerRPC(rpc RPC) *Manager {
	return &Manager{sessions: make(map[string]*Session), rpc: rpc}
}

func (m *Manager) Counts() (sessions, ptys int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.sessions)
	return n, n
}

func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

func (m *Manager) Snapshot(id string) (protocol.ScreenSnapshot, error) {
	s := m.Get(id)
	if s == nil {
		return protocol.ScreenSnapshot{}, fmt.Errorf("unknown session %q", id)
	}
	snap := s.Snapshot()
	return protocol.ScreenSnapshot{
		Rows:      snap.Rows,
		Cols:      snap.Cols,
		CursorRow: snap.CursorRow,
		CursorCol: snap.CursorCol,
		RowANSI:   snap.RowANSI,
	}, nil
}

func (m *Manager) Accept(conn net.Conn) error {
	defer conn.Close()
	req, err := ReadCreate(conn)
	if err != nil {
		return err
	}
	id := uuid.NewString()
	sess, err := startSession(id, req, conn, m.rpc)
	if err != nil {
		_ = WriteFrame(conn, FrameExit, []byte{1})
		return err
	}
	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()
	code := sess.Wait()
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
	var payload [4]byte
	payload[0] = byte(code)
	_ = sess.sendFrame(FrameExit, payload[:])
	return nil
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		s.Close()
		delete(m.sessions, id)
	}
}
