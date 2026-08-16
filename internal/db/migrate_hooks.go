package db

import (
	"crypto/sha256"
	"database/sql"
	"fmt"

	"github.com/dont-be-evil-company/remnix/internal/cborx"
)

var postHooks = map[string]func(*sql.Tx) error{
	"0003_thin_events":     rewriteSyncEvents,
	"0004_history_compact": rewriteHistory,
}

func rewriteSyncEvents(tx *sql.Tx) error {
	if _, err := tx.Exec(`
CREATE TABLE sync_events_new (
    device_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    history_id TEXT,
    payload BLOB,
    applied INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    PRIMARY KEY (device_id, seq)
)`); err != nil {
		return err
	}
	rows, err := tx.Query(`
SELECT device_id, seq, event_type, history_id, payload, applied, created_at
FROM sync_events`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		deviceID, eventType string
		seq                 int64
		historyID           sql.NullString
		payload             []byte
		applied             int
		createdAt           int64
	}
	var copied []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.deviceID, &r.seq, &r.eventType, &r.historyID, &r.payload, &r.applied, &r.createdAt); err != nil {
			return err
		}
		copied = append(copied, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, r := range copied {
		historyID := r.historyID.String
		payload := r.payload
		if len(payload) > 0 {
			ev, err := decodeThinEvent(payload)
			if err == nil {
				if id := historyIDFromThin(ev); id != "" {
					historyID = id
				}
				if ev.Type == "history-created" || ev.Type == "history-tombstoned" {
					payload = nil
				}
			}
		}
		if _, err := tx.Exec(`
INSERT INTO sync_events_new (device_id, seq, event_type, history_id, payload, applied, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, r.deviceID, r.seq, r.eventType, nullIfEmpty(historyID), payload, r.applied, r.createdAt); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DROP TABLE sync_events`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE sync_events_new RENAME TO sync_events`); err != nil {
		return err
	}
	return nil
}

type thinEvent struct {
	Version  int    `cbor:"1,keyasint"`
	Type     string `cbor:"2,keyasint"`
	DeviceID string `cbor:"3,keyasint"`
	Seq      int64  `cbor:"4,keyasint"`
	TimeUnix int64  `cbor:"5,keyasint"`
	Payload  []byte `cbor:"6,keyasint"`
}

func decodeThinEvent(b []byte) (thinEvent, error) {
	var ev thinEvent
	err := cborx.Unmarshal(b, &ev)
	return ev, err
}

func historyIDFromThin(ev thinEvent) string {
	switch ev.Type {
	case "history-created":
		var p struct {
			ID string `cbor:"1,keyasint"`
		}
		if err := cborx.Unmarshal(ev.Payload, &p); err == nil {
			return p.ID
		}
	case "history-tombstoned":
		var p struct {
			HistoryID string `cbor:"1,keyasint"`
		}
		if err := cborx.Unmarshal(ev.Payload, &p); err == nil {
			return p.HistoryID
		}
	}
	return ""
}

func rewriteHistory(tx *sql.Tx) error {
	if _, err := tx.Exec(`
CREATE TABLE history_new (
    id TEXT PRIMARY KEY,
    command TEXT NOT NULL,
    command_hash BLOB NOT NULL,
    start_ts INTEGER NOT NULL,
    end_ts INTEGER,
    duration_ms INTEGER,
    exit_status INTEGER,
    cwd_id INTEGER REFERENCES intern_cwd(id),
    session_id INTEGER REFERENCES intern_session(id),
    hostname_id INTEGER REFERENCES intern_hostname(id),
    device_id TEXT NOT NULL,
    shell_id INTEGER REFERENCES intern_shell(id),
    deleted INTEGER NOT NULL DEFAULT 0,
    origin_device_id TEXT,
    origin_seq INTEGER,
    created_at INTEGER NOT NULL
)`); err != nil {
		return err
	}
	rows, err := tx.Query(`
SELECT id, command, start_ts, end_ts, duration_ms, exit_status, cwd, session_id,
       hostname, device_id, shell, deleted, origin_device_id, origin_seq, created_at
FROM history`)
	if err != nil {
		return fmt.Errorf("read history: %w", err)
	}
	type histRow struct {
		id, command, deviceID string
		startTS, createdAt    int64
		endTS, dur, exit      sql.NullInt64
		cwd, session, host    sql.NullString
		shell                 sql.NullString
		deleted               int
		originDev             sql.NullString
		originSeq             sql.NullInt64
	}
	var copied []histRow
	for rows.Next() {
		var r histRow
		if err := rows.Scan(&r.id, &r.command, &r.startTS, &r.endTS, &r.dur, &r.exit, &r.cwd, &r.session,
			&r.host, &r.deviceID, &r.shell, &r.deleted, &r.originDev, &r.originSeq, &r.createdAt); err != nil {
			_ = rows.Close()
			return err
		}
		copied = append(copied, r)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, r := range copied {
		cwdID, err := internTx(tx, "intern_cwd", r.cwd.String)
		if err != nil {
			return err
		}
		sessID, err := internTx(tx, "intern_session", r.session.String)
		if err != nil {
			return err
		}
		hostID, err := internTx(tx, "intern_hostname", r.host.String)
		if err != nil {
			return err
		}
		shellID, err := internTx(tx, "intern_shell", r.shell.String)
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(r.command))
		if _, err := tx.Exec(`
INSERT INTO history_new (
    id, command, command_hash, start_ts, end_ts, duration_ms, exit_status, cwd_id, session_id,
    hostname_id, device_id, shell_id, deleted, origin_device_id, origin_seq, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			r.id, r.command, sum[:], r.startTS, nullInt(r.endTS), nullInt(r.dur), nullInt(r.exit),
			cwdID, sessID, hostID, r.deviceID, shellID, r.deleted, nullIfEmpty(r.originDev.String), nullInt(r.originSeq), r.createdAt,
		); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DROP TABLE history`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE history_new RENAME TO history`); err != nil {
		return err
	}
	for _, stmt := range []string{
		`CREATE UNIQUE INDEX history_origin ON history(origin_device_id, origin_seq) WHERE origin_device_id IS NOT NULL AND origin_seq IS NOT NULL`,
		`CREATE UNIQUE INDEX history_dedup ON history(command_hash, start_ts, cwd_id, device_id) WHERE deleted = 0`,
		`CREATE INDEX history_command_start ON history(command_hash, start_ts DESC, id) WHERE deleted = 0`,
		`CREATE INDEX history_start_ts ON history(start_ts DESC)`,
		// cwd_id keeps Filter.Cwd (and unique+cwd) index-backed after intern.
		`CREATE INDEX history_cwd ON history(cwd_id)`,
		`CREATE INDEX history_device ON history(device_id)`,
		`CREATE INDEX history_session ON history(session_id)`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func internTx(tx *sql.Tx, table, value string) (any, error) {
	if value == "" {
		return nil, nil
	}
	if _, err := tx.Exec(`INSERT OR IGNORE INTO `+table+` (value) VALUES (?)`, value); err != nil {
		return nil, err
	}
	var id int64
	if err := tx.QueryRow(`SELECT id FROM `+table+` WHERE value = ?`, value).Scan(&id); err != nil {
		return nil, err
	}
	return id, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n sql.NullInt64) any {
	if !n.Valid {
		return nil
	}
	return n.Int64
}
