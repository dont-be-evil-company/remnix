package event

import (
	"database/sql"
	"fmt"

	"github.com/mistweaverco/syncsh/internal/db"
)

type Store struct {
	db *sql.DB
}

func NewStore(d *db.DB) *Store {
	return &Store{db: d.SQL}
}

func (s *Store) NextSeq(deviceID string) (int64, error) {
	var seq sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(seq) FROM sync_events WHERE device_id = ?`, deviceID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	if !seq.Valid {
		return 1, nil
	}
	return seq.Int64 + 1, nil
}

func (s *Store) Append(ev Event) error {
	raw, err := Encode(ev)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
INSERT INTO sync_events (device_id, seq, event_type, payload, applied, created_at)
VALUES (?, ?, ?, ?, 0, ?)`, ev.DeviceID, ev.Seq, ev.Type, raw, ev.TimeUnix)
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *Store) MarkApplied(deviceID string, seq int64) error {
	_, err := s.db.Exec(`UPDATE sync_events SET applied = 1 WHERE device_id = ? AND seq = ?`, deviceID, seq)
	return err
}

func (s *Store) Get(deviceID string, seq int64) (Event, bool, error) {
	var raw []byte
	err := s.db.QueryRow(`SELECT payload FROM sync_events WHERE device_id = ? AND seq = ?`, deviceID, seq).Scan(&raw)
	if err == sql.ErrNoRows {
		return Event{}, false, nil
	}
	if err != nil {
		return Event{}, false, err
	}
	ev, err := Decode(raw)
	return ev, err == nil, err
}

func (s *Store) UnappliedLocal(deviceID string) ([]Event, error) {
	rows, err := s.db.Query(`
SELECT payload FROM sync_events
WHERE device_id = ? AND applied = 0
ORDER BY seq`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) Range(deviceID string, start, end int64) ([]Event, error) {
	rows, err := s.db.Query(`
SELECT payload FROM sync_events
WHERE device_id = ? AND seq >= ? AND seq <= ?
ORDER BY seq`, deviceID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *Store) Exists(deviceID string, seq int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sync_events WHERE device_id = ? AND seq = ?`, deviceID, seq).Scan(&n)
	return n > 0, err
}

func scanEvents(rows *sql.Rows) ([]Event, error) {
	var out []Event
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		ev, err := Decode(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
