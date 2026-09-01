package merge

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mistweaverco/syncsh/internal/db"
)

type HeadStore struct {
	db *sql.DB
}

func NewHeadStore(d *db.DB) *HeadStore {
	return &HeadStore{db: d.SQL}
}

func (s *HeadStore) Get() (Frontier, error) {
	rows, err := s.db.Query(`SELECT device_id, seq FROM sync_heads`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	f := Frontier{}
	for rows.Next() {
		var id string
		var seq int64
		if err := rows.Scan(&id, &seq); err != nil {
			return nil, err
		}
		f[id] = seq
	}
	return f, rows.Err()
}

func (s *HeadStore) Set(deviceID string, seq int64) error {
	_, err := s.db.Exec(`
INSERT INTO sync_heads (device_id, seq) VALUES (?, ?)
ON CONFLICT(device_id) DO UPDATE SET seq = excluded.seq
WHERE excluded.seq > sync_heads.seq`, deviceID, seq)
	return err
}

func (s *HeadStore) AdvanceContiguous(deviceID string) error {
	var head int64
	_ = s.db.QueryRow(`SELECT seq FROM sync_heads WHERE device_id = ?`, deviceID).Scan(&head)
	for {
		next := head + 1
		var n int
		err := s.db.QueryRow(`SELECT COUNT(*) FROM sync_events WHERE device_id = ? AND seq = ? AND applied = 1`, deviceID, next).Scan(&n)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		head = next
		if err := s.Set(deviceID, head); err != nil {
			return err
		}
	}
}

func EncodeFrontier(f Frontier) ([]byte, error) {
	return json.Marshal(f)
}

func DecodeFrontier(b []byte) (Frontier, error) {
	var f Frontier
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("frontier: %w", err)
	}
	if f == nil {
		f = Frontier{}
	}
	return f, nil
}
