package ptyproxy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/client"
	"github.com/dont-be-evil-company/remnix/internal/protocol"
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
func EncodeHeader(s Snapshot) []byte {
	buf := make([]byte, 0, 8)
	buf = binary.BigEndian.AppendUint16(buf, uint16(clampU16(s.Rows)))
	buf = binary.BigEndian.AppendUint16(buf, uint16(clampU16(s.Cols)))
	buf = binary.BigEndian.AppendUint16(buf, uint16(clampU16(s.CursorRow)))
	buf = binary.BigEndian.AppendUint16(buf, uint16(clampU16(s.CursorCol)))
	return buf
}

func EncodeRow(row string) []byte {
	b := []byte(row)
	out := make([]byte, 4+len(b))
	binary.BigEndian.PutUint32(out, uint32(len(b)))
	copy(out[4:], b)
	return out
}

func Encode(s Snapshot) []byte {
	n := len(s.RowANSI)
	buf := EncodeHeader(s)
	if cap(buf) < 8+n*16 {
		buf = append(make([]byte, 0, 8+n*16), buf...)
	}
	for _, row := range s.RowANSI {
		buf = append(buf, EncodeRow(row)...)
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
	if id := os.Getenv(EnvSessionID); id != "" {
		if s, err := fetchDaemonSnapshot(id); err == nil {
			return s, nil
		}
	}
	s, rest, err := FetchGeom()
	if err != nil {
		return Snapshot{}, err
	}
	defer rest.Close()
	setReadDeadline(rest, 2*time.Second)
	rows, err := ReadRows(rest, s.Rows)
	if err != nil {
		return Snapshot{}, err
	}
	s.RowANSI = rows
	return s, nil
}

// FetchGeom reads only the 8-byte header (size + cursor). The proxy writes
// that before encoding row ANSI, so the overlay can paint immediately.
// The caller must Close the returned reader (and may ReadRows from it).
func FetchGeom() (Snapshot, io.ReadCloser, error) {
	path := SocketPath()
	if path == "" {
		return fetchGeomFromDaemon()
	}
	conn, err := net.DialTimeout("unix", path, 50*time.Millisecond)
	if err != nil {
		return Snapshot{}, nil, err
	}
	if uc, ok := conn.(*net.UnixConn); ok {
		_ = uc.CloseWrite()
	}
	_ = conn.SetDeadline(time.Now().Add(50 * time.Millisecond))
	s, err := DecodeHeader(conn)
	if err != nil {
		_ = conn.Close()
		return Snapshot{}, nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return s, conn, nil
}

func fetchGeomFromDaemon() (Snapshot, io.ReadCloser, error) {
	id := os.Getenv(EnvSessionID)
	if id == "" {
		return Snapshot{}, nil, ErrNoProxy
	}
	s, err := fetchDaemonSnapshot(id)
	if err != nil {
		return Snapshot{}, nil, err
	}
	var buf bytes.Buffer
	for _, row := range s.RowANSI {
		buf.Write(EncodeRow(row))
	}
	return s, io.NopCloser(&buf), nil
}

func setReadDeadline(r io.ReadCloser, d time.Duration) {
	if c, ok := r.(interface{ SetDeadline(time.Time) error }); ok {
		_ = c.SetDeadline(time.Now().Add(d))
	}
}

func DecodeHeader(r io.Reader) (Snapshot, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Snapshot{}, err
	}
	s := Snapshot{
		Rows:      int(binary.BigEndian.Uint16(hdr[0:2])),
		Cols:      int(binary.BigEndian.Uint16(hdr[2:4])),
		CursorRow: int(binary.BigEndian.Uint16(hdr[4:6])),
		CursorCol: int(binary.BigEndian.Uint16(hdr[6:8])),
	}
	if s.Rows < 0 {
		s.Rows = 0
	}
	return s, nil
}

func ReadRows(r io.Reader, n int) ([]string, error) {
	if n < 0 {
		n = 0
	}
	rows := make([]string, 0, n)
	var lenBuf [4]byte
	for i := 0; i < n; i++ {
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			return nil, err
		}
		sz := int(binary.BigEndian.Uint32(lenBuf[:]))
		if sz < 0 {
			return nil, fmt.Errorf("snapshot: negative row length")
		}
		if sz == 0 {
			rows = append(rows, "")
			continue
		}
		buf := make([]byte, sz)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		rows = append(rows, string(buf))
	}
	return rows, nil
}

// DecodeFrom reads one snapshot from r. Unlike io.ReadAll, it returns as
// soon as every row is present and does not wait for EOF.
func DecodeFrom(r io.Reader) (Snapshot, error) {
	s, err := DecodeHeader(r)
	if err != nil {
		return Snapshot{}, err
	}
	rows, err := ReadRows(r, s.Rows)
	if err != nil {
		return Snapshot{}, err
	}
	s.RowANSI = rows
	return s, nil
}

func fetchDaemonSnapshot(id string) (Snapshot, error) {
	c, err := client.Dial()
	if err != nil {
		return Snapshot{}, err
	}
	defer c.Close()
	var snap protocol.ScreenSnapshot
	if err := c.CallSession(protocol.OpScreenSnapshot, id, nil, &snap); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Rows:      snap.Rows,
		Cols:      snap.Cols,
		CursorRow: snap.CursorRow,
		CursorCol: snap.CursorCol,
		RowANSI:   snap.RowANSI,
	}, nil
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
