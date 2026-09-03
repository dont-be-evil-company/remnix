package merge

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/mistweaverco/syncsh/internal/cborx"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/sync/event"
)

func Apply(tx *sql.Tx, ev event.Event) error {
	switch ev.Type {
	case event.TypeHistoryCreated:
		return applyCreated(tx, ev)
	case event.TypeHistoryTombstoned:
		return applyTombstone(tx, ev)
	case event.TypeDeviceMeta:
		return applyDeviceMeta(tx, ev)
	case event.TypeCheckpointRef:
		return nil
	default:
		return fmt.Errorf("unknown event type %q", ev.Type)
	}
}

func applyCreated(tx *sql.Tx, ev event.Event) error {
	p, err := event.DecodeHistoryCreated(ev)
	if err != nil {
		return err
	}
	var deleted int
	err = tx.QueryRow(`SELECT deleted FROM history WHERE origin_device_id = ? AND origin_seq = ?`, ev.DeviceID, ev.Seq).Scan(&deleted)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	e := history.Entry{
		ID:             p.ID,
		Command:        p.Command,
		StartTS:        time.UnixMilli(p.StartTS).UTC(),
		Cwd:            p.Cwd,
		SessionID:      p.SessionID,
		Hostname:       p.Hostname,
		DeviceID:       ev.DeviceID,
		Shell:          p.Shell,
		OriginDeviceID: ev.DeviceID,
		OriginSeq:      &ev.Seq,
		ExitStatus:     p.ExitStatus,
	}
	if p.EndTS > 0 {
		t := time.UnixMilli(p.EndTS).UTC()
		e.EndTS = &t
	}
	if p.DurationMs > 0 {
		d := p.DurationMs
		e.DurationMs = &d
	}
	_, err = history.InsertTx(tx, e)
	return err
}

func applyTombstone(tx *sql.Tx, ev event.Event) error {
	p, err := event.DecodeHistoryTombstoned(ev)
	if err != nil {
		return err
	}
	if p.HistoryID != "" {
		if _, err := tx.Exec(`UPDATE history SET deleted = 1 WHERE id = ?`, p.HistoryID); err != nil {
			return err
		}
	}
	if p.OriginDeviceID != "" && p.OriginSeq > 0 {
		res, err := tx.Exec(`UPDATE history SET deleted = 1 WHERE origin_device_id = ? AND origin_seq = ?`, p.OriginDeviceID, p.OriginSeq)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			id := p.HistoryID
			if id == "" {
				id = fmt.Sprintf("tombstone:%s:%d", p.OriginDeviceID, p.OriginSeq)
			}
			seq := p.OriginSeq
			_, err = history.InsertTx(tx, history.Entry{
				ID:             id,
				StartTS:        time.UnixMilli(0).UTC(),
				DeviceID:       p.OriginDeviceID,
				Deleted:        true,
				OriginDeviceID: p.OriginDeviceID,
				OriginSeq:      &seq,
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func applyDeviceMeta(tx *sql.Tx, ev event.Event) error {
	var p event.DeviceMeta
	if err := cborx.Unmarshal(ev.Payload, &p); err != nil {
		return err
	}
	status := p.Status
	if status == "" {
		status = "active"
	}
	_, err := tx.Exec(`
INSERT INTO devices (id, name, hostname, status, created_at, retired_at)
VALUES (?, ?, ?, ?, ?, NULL)
ON CONFLICT(id) DO UPDATE SET
    name = excluded.name,
    hostname = excluded.hostname,
    status = CASE WHEN devices.status = 'retired' AND excluded.status = 'active' THEN devices.status ELSE excluded.status END,
    retired_at = CASE WHEN devices.status = 'retired' AND excluded.status = 'active' THEN devices.retired_at ELSE excluded.retired_at END`,
		ev.DeviceID, p.Name, nullString(p.Hostname), status, time.Now().UnixMilli(),
	)
	return err
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
