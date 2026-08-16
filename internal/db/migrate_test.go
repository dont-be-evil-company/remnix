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
