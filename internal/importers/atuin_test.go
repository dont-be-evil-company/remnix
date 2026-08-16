package importers

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestImportAtuin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE history (
		id text, timestamp integer, duration integer, exit integer,
		command text, cwd text, session text, hostname text
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO history VALUES ('id1', 1700000000, 1500, 0, 'ls', '/tmp', 's1', 'host')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	got, err := ImportAtuin(path, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "ls" || got[0].Cwd != "/tmp" {
		t.Fatalf("%+v", got)
	}
}
