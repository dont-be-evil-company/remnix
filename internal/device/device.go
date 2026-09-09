package device

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/db"
	"github.com/google/uuid"
)

const (
	StatusActive  = "active"
	StatusRetired = "retired"
)

type Device struct {
	ID        string
	Name      string
	Hostname  string
	Status    string
	CreatedAt time.Time
	RetiredAt *time.Time
}

func NewID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func DefaultName() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unnamed"
	}
	return h
}

type Store struct {
	db *sql.DB
}

func NewStore(d *db.DB) *Store {
	return &Store{db: d.SQL}
}

func (s *Store) Upsert(dev Device) error {
	if dev.CreatedAt.IsZero() {
		dev.CreatedAt = time.Now().UTC()
	}
	if dev.Status == "" {
		dev.Status = StatusActive
	}
	_, err := s.db.Exec(`
INSERT INTO devices (id, name, hostname, status, created_at, retired_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    hostname = excluded.hostname,
    status = CASE WHEN devices.status = 'retired' AND excluded.status = 'active' THEN devices.status ELSE excluded.status END,
    retired_at = CASE WHEN devices.status = 'retired' AND excluded.status = 'active' THEN devices.retired_at ELSE excluded.retired_at END`,
		dev.ID, dev.Name, nullString(dev.Hostname), dev.Status, dev.CreatedAt.UnixMilli(), unixMilliPtr(dev.RetiredAt),
	)
	return err
}

func (s *Store) Get(id string) (Device, bool, error) {
	row := s.db.QueryRow(`SELECT id, name, hostname, status, created_at, retired_at FROM devices WHERE id = ?`, id)
	d, err := scanDevice(row)
	if err == sql.ErrNoRows {
		return Device{}, false, nil
	}
	if err != nil {
		return Device{}, false, err
	}
	return d, true, nil
}

func (s *Store) List() ([]Device, error) {
	rows, err := s.db.Query(`SELECT id, name, hostname, status, created_at, retired_at FROM devices ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Retire(id string, at time.Time) error {
	res, err := s.db.Exec(`UPDATE devices SET status = ?, retired_at = ? WHERE id = ?`, StatusRetired, at.UnixMilli(), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("device %s not found", id)
	}
	return nil
}

func (s *Store) Delete(id string) error {
	res, err := s.db.Exec(`DELETE FROM devices WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("device %s not found", id)
	}
	return nil
}

func (s *Store) ActiveIDs() ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM devices WHERE status = ?`, StatusActive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDevice(row rowScanner) (Device, error) {
	var (
		d         Device
		hostname  sql.NullString
		createdMS int64
		retiredMS sql.NullInt64
	)
	if err := row.Scan(&d.ID, &d.Name, &hostname, &d.Status, &createdMS, &retiredMS); err != nil {
		return Device{}, err
	}
	d.Hostname = hostname.String
	d.CreatedAt = time.UnixMilli(createdMS).UTC()
	if retiredMS.Valid {
		t := time.UnixMilli(retiredMS.Int64).UTC()
		d.RetiredAt = &t
	}
	return d, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func unixMilliPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}
