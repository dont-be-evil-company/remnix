package ptyproxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// Snapshot is a visible-screen capture from pty-proxy. RowANSI entries are
// pre-formatted (SGR + text) and can be written straight to a tty.
type Snapshot struct {
	Rows, Cols int
	CursorRow  int
	CursorCol  int
	RowANSI    []string
}

var ErrNoProxy = errors.New("pty-proxy is not active")

// Encode writes the Atuin-compatible snapshot wire format:
//
//	[rows u16 BE][cols u16 BE][cursor_row u16 BE][cursor_col u16 BE]
//	[row_len u32 BE][row bytes] × N
func Encode(s Snapshot) []byte {
	n := len(s.RowANSI)
	buf := make([]byte, 0, 8+n*16)
	buf = binary.BigEndian.AppendUint16(buf, uint16(clampU16(s.Rows)))
	buf = binary.BigEndian.AppendUint16(buf, uint16(clampU16(s.Cols)))
	buf = binary.BigEndian.AppendUint16(buf, uint16(clampU16(s.CursorRow)))
	buf = binary.BigEndian.AppendUint16(buf, uint16(clampU16(s.CursorCol)))
	for _, row := range s.RowANSI {
		b := []byte(row)
		buf = binary.BigEndian.AppendUint32(buf, uint32(len(b)))
		buf = append(buf, b...)
	}
	return buf
}

// Decode parses a snapshot blob produced by Encode.
func Decode(data []byte) (Snapshot, error) {
	if len(data) < 8 {
		return Snapshot{}, fmt.Errorf("snapshot: short header (%d bytes)", len(data))
	}
	s := Snapshot{
		Rows:      int(binary.BigEndian.Uint16(data[0:2])),
		Cols:      int(binary.BigEndian.Uint16(data[2:4])),
		CursorRow: int(binary.BigEndian.Uint16(data[4:6])),
		CursorCol: int(binary.BigEndian.Uint16(data[6:8])),
	}
	rest := data[8:]
	want := s.Rows
	if want < 0 {
		want = 0
	}
	s.RowANSI = make([]string, 0, want)
	for len(rest) >= 4 {
		n := int(binary.BigEndian.Uint32(rest[:4]))
		rest = rest[4:]
		if n < 0 || n > len(rest) {
			return Snapshot{}, fmt.Errorf("snapshot: truncated row (want %d, have %d)", n, len(rest))
		}
		s.RowANSI = append(s.RowANSI, string(rest[:n]))
		rest = rest[n:]
	}
	if len(rest) != 0 {
		return Snapshot{}, fmt.Errorf("snapshot: trailing %d bytes", len(rest))
	}
	return s, nil
}

// Fetch reads the current screen from the wrapping pty-proxy. Write-shutdown
// is used so an older proxy waiting for a request byte unblocks immediately.
func Fetch() (Snapshot, error) {
	path := SocketPath()
	if path == "" {
		return Snapshot{}, ErrNoProxy
	}
	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err != nil {
		return Snapshot{}, err
	}
	defer conn.Close()
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	data, err := io.ReadAll(conn)
	if err != nil {
		return Snapshot{}, err
	}
	return Decode(data)
}

func clampU16(n int) int {
	if n < 0 {
		return 0
	}
	if n > 0xffff {
		return 0xffff
	}
	return n
}
