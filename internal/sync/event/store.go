package event

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/dont-be-evil-company/remnix/internal/db"
	"github.com/dont-be-evil-company/remnix/internal/history"
)

type Store struct {
	db *sql.DB
}

func NewStore(d *db.DB) *Store {
	return &Store{db: d.SQL}
}

type dbtx interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func (s *Store) NextSeq(deviceID string) (int64, error) {
	var eventMax, headMax sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(seq) FROM sync_events WHERE device_id = ?`, deviceID).Scan(&eventMax); err != nil {
		return 0, err
	}
	_ = s.db.QueryRow(`SELECT seq FROM sync_heads WHERE device_id = ?`, deviceID).Scan(&headMax)
	n := int64(0)
	if eventMax.Valid {
		n = eventMax.Int64
	}
	if headMax.Valid && headMax.Int64 > n {
		n = headMax.Int64
	}
	return n + 1, nil
}

func (s *Store) Append(ev Event) error {
	return AppendExec(s.db, ev)
}

func AppendTx(tx *sql.Tx, ev Event) error {
	return AppendExec(tx, ev)
}

func AppendExec(eq dbtx, ev Event) error {
	historyID := historyIDOf(ev)
	var payload []byte
	if ev.Type != TypeHistoryCreated && ev.Type != TypeHistoryTombstoned {
		raw, err := Encode(ev)
		if err != nil {
			return err
		}
		payload = raw
	}
	_, err := eq.Exec(`
INSERT INTO sync_events (device_id, seq, event_type, history_id, payload, applied, created_at)
VALUES (?, ?, ?, ?, ?, 0, ?)`, ev.DeviceID, ev.Seq, ev.Type, nullIfEmpty(historyID), payload, ev.TimeUnix)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *Store) MarkApplied(deviceID string, seq int64) error {
	_, err := s.db.Exec(`UPDATE sync_events SET applied = 1 WHERE device_id = ? AND seq = ?`, deviceID, seq)
	return err
}

func MarkAppliedTx(tx *sql.Tx, deviceID string, seq int64) error {
	_, err := tx.Exec(`UPDATE sync_events SET applied = 1 WHERE device_id = ? AND seq = ?`, deviceID, seq)
	return err
}

func (s *Store) Get(deviceID string, seq int64) (Event, bool, error) {
	ev, ok, err := s.loadRow(s.db, deviceID, seq)
	if err != nil || ok {
		return ev, ok, err
	}
	ev, err = s.rebuildCreated(deviceID, seq, "")
	if err != nil {
		return Event{}, false, err
	}
	if ev.Seq == 0 {
		return Event{}, false, nil
	}
	return ev, true, nil
}

func (s *Store) UnappliedLocal(deviceID string) ([]Event, error) {
	rows, err := s.db.Query(`
SELECT seq, event_type, history_id, payload, created_at FROM sync_events
WHERE device_id = ? AND applied = 0
ORDER BY seq`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanHydrated(rows, deviceID)
}

func (s *Store) Range(deviceID string, start, end int64) ([]Event, error) {
	rows, err := s.db.Query(`
SELECT seq, event_type, history_id, payload, created_at FROM sync_events
WHERE device_id = ? AND seq >= ? AND seq <= ?
ORDER BY seq`, deviceID, start, end)
	if err != nil {
		return nil, err
	}
	stored, err := s.scanHydrated(rows, deviceID)
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	bySeq := make(map[int64]Event, len(stored))
	for _, ev := range stored {
		bySeq[ev.Seq] = ev
	}
	hs := history.NewSQLStore(s.db)
	entries, err := hs.ListByOriginRange(deviceID, start, end)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.OriginSeq == nil {
			continue
		}
		seq := *e.OriginSeq
		if _, ok := bySeq[seq]; ok {
			continue
		}
		ev, err := NewHistoryCreated(deviceID, seq, e)
		if err != nil {
			return nil, err
		}
		ev.TimeUnix = e.CreatedAt.UnixMilli()
		bySeq[seq] = ev
	}
	seqs := make([]int64, 0, len(bySeq))
	for seq := range bySeq {
		seqs = append(seqs, seq)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	out := make([]Event, 0, len(seqs))
	for _, seq := range seqs {
		out = append(out, bySeq[seq])
	}
	return out, nil
}

func (s *Store) Exists(deviceID string, seq int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sync_events WHERE device_id = ? AND seq = ?`, deviceID, seq).Scan(&n)
	return n > 0, err
}

func ExistsTx(tx *sql.Tx, deviceID string, seq int64) (bool, error) {
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM sync_events WHERE device_id = ? AND seq = ?`, deviceID, seq).Scan(&n)
	return n > 0, err
}

func (s *Store) CompactCreated(deviceID string, throughSeq int64) error {
	_, err := s.db.Exec(`
DELETE FROM sync_events
WHERE device_id = ? AND seq <= ? AND event_type = ? AND applied = 1`,
		deviceID, throughSeq, TypeHistoryCreated)
	return err
}

func (s *Store) CompactUpToHeads() error {
	_, err := s.db.Exec(`
DELETE FROM sync_events
WHERE event_type = ?
  AND applied = 1
  AND seq <= IFNULL((SELECT seq FROM sync_heads h WHERE h.device_id = sync_events.device_id), 0)`,
		TypeHistoryCreated)
	return err
}

func (s *Store) loadRow(eq dbtx, deviceID string, seq int64) (Event, bool, error) {
	var eventType string
	var historyID sql.NullString
	var payload []byte
	var createdAt int64
	err := eq.QueryRow(`
SELECT event_type, history_id, payload, created_at FROM sync_events
WHERE device_id = ? AND seq = ?`, deviceID, seq).Scan(&eventType, &historyID, &payload, &createdAt)
	if err == sql.ErrNoRows {
		return Event{}, false, nil
	}
	if err != nil {
		return Event{}, false, err
	}
	ev, err := s.hydrate(deviceID, seq, eventType, historyID.String, payload, createdAt)
	return ev, err == nil, err
}

func (s *Store) scanHydrated(rows *sql.Rows, deviceID string) ([]Event, error) {
	var out []Event
	for rows.Next() {
		var seq, createdAt int64
		var eventType string
		var historyID sql.NullString
		var payload []byte
		if err := rows.Scan(&seq, &eventType, &historyID, &payload, &createdAt); err != nil {
			return nil, err
		}
		ev, err := s.hydrate(deviceID, seq, eventType, historyID.String, payload, createdAt)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) hydrate(deviceID string, seq int64, eventType, historyID string, payload []byte, createdAt int64) (Event, error) {
	if len(payload) > 0 {
		return Decode(payload)
	}
	switch eventType {
	case TypeHistoryCreated:
		ev, err := s.rebuildCreated(deviceID, seq, historyID)
		if err != nil {
			return Event{}, err
		}
		if createdAt != 0 {
			ev.TimeUnix = createdAt
		}
		return ev, nil
	case TypeHistoryTombstoned:
		return s.rebuildTombstone(deviceID, seq, historyID, createdAt)
	default:
		return Event{}, fmt.Errorf("missing payload for %s %s/%d", eventType, deviceID, seq)
	}
}

func (s *Store) rebuildCreated(deviceID string, seq int64, historyID string) (Event, error) {
	hs := history.NewSQLStore(s.db)
	var e history.Entry
	var ok bool
	var err error
	if historyID != "" {
		e, ok, err = hs.Get(historyID)
	}
	if err != nil {
		return Event{}, err
	}
	if !ok {
		e, ok, err = hs.ByOrigin(deviceID, seq)
		if err != nil {
			return Event{}, err
		}
	}
	if !ok {
		return Event{}, nil
	}
	ev, err := NewHistoryCreated(deviceID, seq, e)
	if err != nil {
		return Event{}, err
	}
	ev.TimeUnix = e.CreatedAt.UnixMilli()
	return ev, nil
}

func (s *Store) rebuildTombstone(deviceID string, seq int64, historyID string, createdAt int64) (Event, error) {
	hs := history.NewSQLStore(s.db)
	e, ok, err := hs.Get(historyID)
	if err != nil {
		return Event{}, err
	}
	var originSeq int64
	var originDev string
	if ok {
		originDev = e.OriginDeviceID
		if e.OriginSeq != nil {
			originSeq = *e.OriginSeq
		}
		if historyID == "" {
			historyID = e.ID
		}
	}
	ev, err := NewHistoryTombstoned(deviceID, seq, HistoryTombstoned{
		HistoryID:      historyID,
		OriginDeviceID: originDev,
		OriginSeq:      originSeq,
	})
	if err != nil {
		return Event{}, err
	}
	if createdAt != 0 {
		ev.TimeUnix = createdAt
	}
	return ev, nil
}

func historyIDOf(ev Event) string {
	switch ev.Type {
	case TypeHistoryCreated:
		p, err := DecodeHistoryCreated(ev)
		if err == nil {
			return p.ID
		}
	case TypeHistoryTombstoned:
		p, err := DecodeHistoryTombstoned(ev)
		if err == nil {
			return p.HistoryID
		}
	}
	return ""
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
