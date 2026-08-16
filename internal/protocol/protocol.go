package protocol

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/dont-be-evil-company/remnix/internal/cborx"
)

const (
	Version = 1

	OpPing           = "ping"
	OpVersion        = "version"
	OpCapabilities   = "capabilities"
	OpHistoryStart   = "history.start"
	OpHistoryEnd     = "history.end"
	OpSuggest        = "suggest"
	OpSuggestList    = "suggest-list"
	OpHistorySearch  = "history.search"
	OpHistoryDelete  = "history.delete"
	OpHistoryTombEnt = "history.tombstone-entries"
	OpHistoryImport  = "history.import"
	OpHistoryStats   = "history.stats"
	OpSyncNow        = "sync.now"
	OpDaemonStats    = "daemon.stats"
	OpDaemonStatus   = "daemon.status"
	OpReloadConfig   = "config.reload"
	OpCompactCache   = "cache.compact"
	OpScreenSnapshot = "screen.snapshot"

	StatusOK  = "ok"
	StatusErr = "err"
)

type Request struct {
	Version   int    `cbor:"v"`
	ID        uint64 `cbor:"id"`
	Op        string `cbor:"op"`
	SessionID string `cbor:"sid,omitempty"`
	Payload   []byte `cbor:"p,omitempty"`
}

type Response struct {
	ID      uint64 `cbor:"id"`
	Status  string `cbor:"st"`
	Error   string `cbor:"err,omitempty"`
	Payload []byte `cbor:"p,omitempty"`
}

type SuggestReq struct {
	Prefix string `cbor:"prefix"`
	Cwd    string `cbor:"cwd"`
}

type SuggestListReq struct {
	Prefix string `cbor:"prefix"`
	Cwd    string `cbor:"cwd"`
	Limit  int    `cbor:"limit,omitempty"`
}

type SuggestListRes struct {
	Items []string `cbor:"items"`
}

type HistoryStartReq struct {
	Command string `cbor:"command"`
	Cwd     string `cbor:"cwd"`
	Session string `cbor:"session"`
	Shell   string `cbor:"shell"`
}

type HistoryStartRes struct {
	ID string `cbor:"id"`
}

type HistoryEndReq struct {
	ID   string `cbor:"id"`
	Exit int    `cbor:"exit"`
}

type HistorySearchReq struct {
	Query     string `cbor:"query"`
	Cwd       string `cbor:"cwd"`
	DeviceID  string `cbor:"device,omitempty"`
	SessionID string `cbor:"session,omitempty"`
	Host      string `cbor:"host,omitempty"`
	Shell     string `cbor:"shell,omitempty"`
	Limit     int    `cbor:"limit"`
	Exact     bool   `cbor:"exact"`
	Unique    bool   `cbor:"unique"`
}

type SearchHit struct {
	ID        string `cbor:"id"`
	Command   string `cbor:"command"`
	Cwd       string `cbor:"cwd"`
	DeviceID  string `cbor:"device"`
	SessionID string `cbor:"session"`
	Hostname  string `cbor:"host"`
	Shell     string `cbor:"shell"`
	StartTS   int64  `cbor:"start_ts"`
	Exit      *int   `cbor:"exit,omitempty"`
}

type HistorySearchRes struct {
	Hits []SearchHit `cbor:"hits"`
}

type HistoryDeleteReq struct {
	Command string `cbor:"command"`
}

type HistoryTombstoneReq struct {
	IDs []string `cbor:"ids"`
}

type SyncNowReq struct {
	Checkpoint bool `cbor:"checkpoint,omitempty"`
}

type Capabilities struct {
	Protocol  int      `cbor:"protocol"`
	Ops       []string `cbor:"ops"`
	NULCompat bool     `cbor:"nul"`
	HasCache  bool     `cbor:"cache"`
	HasPty    bool     `cbor:"pty"`
	HasTheme  bool     `cbor:"theme"`
}

type VersionRes struct {
	Protocol int    `cbor:"protocol"`
	Version  string `cbor:"version"`
}

type Stats struct {
	PID           int    `cbor:"pid"`
	UptimeSec     int64  `cbor:"uptime_sec"`
	HeapAlloc     uint64 `cbor:"heap_alloc"`
	RSSBytes      uint64 `cbor:"rss_bytes,omitempty"`
	Sessions      int    `cbor:"sessions"`
	ActivePTYs    int    `cbor:"active_ptys"`
	CacheEntries  int    `cbor:"cache_entries"`
	CacheBytes    int64  `cbor:"cache_bytes"`
	CacheInterned int    `cbor:"cache_interned,omitempty"`
	CacheDirty    bool   `cbor:"cache_dirty"`
	DBOpenConns   int    `cbor:"db_open_conns"`
	DBInUse       int    `cbor:"db_in_use"`
	LastSyncAt    int64  `cbor:"last_sync_at,omitempty"`
	LastSyncOK    bool   `cbor:"last_sync_ok"`
	LastSyncClass string `cbor:"last_sync_class,omitempty"`
	LastSyncError string `cbor:"last_sync_error,omitempty"`
	SyncStage     string `cbor:"sync_stage,omitempty"`
}

const maxFrame = 16 << 20

func WriteFrame(w io.Writer, v any) error {
	body, err := cborx.Marshal(v)
	if err != nil {
		return err
	}
	if len(body) > maxFrame {
		return fmt.Errorf("protocol: frame %d exceeds %d", len(body), maxFrame)
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(body)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

func ReadFrame(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n == 0 {
		return fmt.Errorf("protocol: empty frame")
	}
	if n > maxFrame {
		return fmt.Errorf("protocol: frame %d exceeds %d", n, maxFrame)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return err
	}
	return cborx.Unmarshal(body, v)
}

func MarshalPayload(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return cborx.Marshal(v)
}

func UnmarshalPayload(b []byte, v any) error {
	if len(b) == 0 {
		return nil
	}
	return cborx.Unmarshal(b, v)
}

func EncodeOK(id uint64, payload any) (Response, error) {
	var body []byte
	var err error
	if payload != nil {
		body, err = MarshalPayload(payload)
		if err != nil {
			return Response{}, err
		}
	}
	return Response{ID: id, Status: StatusOK, Payload: body}, nil
}

func EncodeErr(id uint64, msg string) Response {
	return Response{ID: id, Status: StatusErr, Error: msg}
}

type ScreenSnapshot struct {
	Rows      int      `cbor:"rows"`
	Cols      int      `cbor:"cols"`
	CursorRow int      `cbor:"cursor_row"`
	CursorCol int      `cbor:"cursor_col"`
	RowANSI   []string `cbor:"rows_ansi"`
}
