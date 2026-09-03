package db_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/db"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/sync/event"
)

func TestMigrateThinsExistingEvents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.SQL.Exec(`
CREATE TABLE schema_migrations (id TEXT PRIMARY KEY, checksum TEXT NOT NULL, applied_at INTEGER NOT NULL);
CREATE TABLE devices (id TEXT PRIMARY KEY, name TEXT NOT NULL, hostname TEXT, status TEXT NOT NULL, created_at INTEGER NOT NULL, retired_at INTEGER);
CREATE TABLE history (
    id TEXT PRIMARY KEY, command TEXT NOT NULL, start_ts INTEGER NOT NULL, end_ts INTEGER,
    duration_ms INTEGER, exit_status INTEGER, cwd TEXT, session_id TEXT, hostname TEXT,
    device_id TEXT NOT NULL, shell TEXT, deleted INTEGER NOT NULL DEFAULT 0,
    origin_device_id TEXT, origin_seq INTEGER, created_at INTEGER NOT NULL
);
CREATE TABLE sync_events (
    device_id TEXT NOT NULL, seq INTEGER NOT NULL, event_type TEXT NOT NULL,
    payload BLOB NOT NULL, applied INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL,
    PRIMARY KEY (device_id, seq)
);
CREATE TABLE sync_heads (device_id TEXT PRIMARY KEY, seq INTEGER NOT NULL);
CREATE TABLE acks (device_id TEXT PRIMARY KEY, frontier_json TEXT NOT NULL, checkpoint_id TEXT, updated_at INTEGER NOT NULL);
CREATE TABLE checkpoints (id TEXT PRIMARY KEY, generation_id TEXT NOT NULL, frontier_json TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE key_generations (id TEXT PRIMARY KEY, seq INTEGER NOT NULL UNIQUE, status TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE key_slots (id TEXT PRIMARY KEY, generation_id TEXT NOT NULL, slot_type TEXT NOT NULL, wrap_params BLOB, wrapped_smk BLOB NOT NULL, status TEXT NOT NULL, created_at INTEGER NOT NULL);
CREATE TABLE trusted_manifests (kind TEXT PRIMARY KEY, generation_id TEXT, counter INTEGER NOT NULL, checkpoint_id TEXT, payload BLOB, updated_at INTEGER NOT NULL);
CREATE TABLE transport_state (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`); err != nil {
		t.Fatal(err)
	}
	migs, err := db.LoadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migs {
		if m.ID >= "0003_thin_events" {
			break
		}
		if _, err := d.SQL.Exec(`INSERT INTO schema_migrations (id, checksum, applied_at) VALUES (?, ?, ?)`, m.ID, m.Checksum, time.Now().Unix()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.SQL.Exec(`
INSERT INTO history (id, command, start_ts, cwd, device_id, deleted, created_at)
VALUES ('h1', 'ls -la', 1000, '/tmp', 'dev', 0, 1000)`); err != nil {
		t.Fatal(err)
	}
	ev, err := event.NewHistoryCreated("dev", 1, history.Entry{
		ID: "h1", Command: "ls -la", StartTS: time.UnixMilli(1000).UTC(), Cwd: "/tmp", DeviceID: "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := event.Encode(ev)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`
INSERT INTO sync_events (device_id, seq, event_type, payload, applied, created_at)
VALUES ('dev', 1, ?, ?, 1, 1000)`, ev.Type, raw); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(d.SQL); err != nil {
		t.Fatal(err)
	}
	n, err := db.HistoryEventPayloads(d.SQL)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("payloads remaining %d", n)
	}
	var historyID string
	if err := d.SQL.QueryRow(`SELECT history_id FROM sync_events WHERE device_id='dev' AND seq=1`).Scan(&historyID); err != nil {
		t.Fatal(err)
	}
	if historyID != "h1" {
		t.Fatalf("history_id %q", historyID)
	}
	var cwd string
	if err := d.SQL.QueryRow(`
SELECT intern_cwd.value FROM history h
LEFT JOIN intern_cwd ON intern_cwd.id = h.cwd_id
WHERE h.id = 'h1'`).Scan(&cwd); err != nil {
		t.Fatal(err)
	}
	if cwd != "/tmp" {
		t.Fatalf("cwd %q", cwd)
	}
}
