package db

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateFromEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	d, err := OpenAndMigrate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	st, err := MigrationStatus(d.SQL)
	if err != nil {
		t.Fatal(err)
	}
	if len(st) == 0 {
		t.Fatal("expected migrations")
	}
	for _, s := range st {
		if !s.Applied {
			t.Fatalf("migration %s not applied", s.ID)
		}
		if s.Mismatch {
			t.Fatalf("migration %s checksum mismatch", s.ID)
		}
	}

	var name string
	if err := d.SQL.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='history'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	var hashCol int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('history') WHERE name='command_hash'`).Scan(&hashCol); err != nil || hashCol != 1 {
		t.Fatalf("command_hash column missing: %v", err)
	}
	for _, intern := range []string{"intern_cwd", "intern_session", "intern_hostname", "intern_shell"} {
		if err := d.SQL.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, intern).Scan(&name); err != nil {
			t.Fatalf("missing %s: %v", intern, err)
		}
	}
	for _, idx := range []string{"history_dedup", "history_command_start", "history_cwd"} {
		if err := d.SQL.QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name=?`, idx).Scan(&name); err != nil {
			t.Fatalf("missing index %s: %v", idx, err)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	d, err := OpenAndMigrate(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(d.SQL); err != nil {
		t.Fatal(err)
	}
	d.Close()
}

func TestChecksumMismatchFailsLoudly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	d, err := OpenAndMigrate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.SQL.Exec(`UPDATE schema_migrations SET checksum = 'deadbeef'`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(d.SQL); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
}

func TestIntegrityCheckOK(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	d, err := OpenAndMigrate(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	got, err := IntegrityCheck(d.SQL)
	if err != nil {
		t.Fatal(err)
	}
	if got != "ok" {
		t.Fatalf("integrity_check = %q", got)
	}
}

func TestOpenCreatesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "history.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestFormatBytes(t *testing.T) {
	if got := FormatBytes(500); got != "500 B" {
		t.Fatalf("%s", got)
	}
	if got := FormatBytes(2048); got != "2.0 KiB" {
		t.Fatalf("%s", got)
	}
}

func TestCompactAndBTreeStats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	d, err := OpenAndMigrate(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`INSERT INTO intern_cwd (value) VALUES ('/tmp')`); err != nil {
		t.Fatal(err)
	}
	if err := Compact(d.SQL); err != nil {
		t.Fatal(err)
	}
	info, err := PageInfoOf(d.SQL)
	if err != nil {
		t.Fatal(err)
	}
	if info.PageCount < 1 || info.Bytes < 1 {
		t.Fatalf("%+v", info)
	}
	stats, err := BTreeStats(d.SQL)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) == 0 {
		t.Fatal("expected dbstat rows")
	}
	n, err := HistoryEventPayloads(d.SQL)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("payloads %d", n)
	}
	d.Close()
}

func TestCheckpointWALOnClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.db")
	d, err := OpenAndMigrate(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.SQL.Exec(`CREATE TABLE IF NOT EXISTS intern_cwd (id INTEGER PRIMARY KEY, value TEXT NOT NULL UNIQUE)`); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	wal := path + "-wal"
	if st, err := os.Stat(wal); err == nil && st.Size() > 1024 {
		t.Fatalf("wal still large: %d", st.Size())
	}
}
