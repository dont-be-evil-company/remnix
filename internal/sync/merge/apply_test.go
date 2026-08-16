package merge

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/db"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/sync/event"
)

func TestApplyOrderConverges(t *testing.T) {
	mk := func(t *testing.T) *sql.Tx {
		d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "h.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.Close() })
		tx, err := d.SQL.Begin()
		if err != nil {
			t.Fatal(err)
		}
		return tx
	}
	created, _ := event.NewHistoryCreated("a", 1, history.Entry{ID: "h", Command: "ls", StartTS: time.Unix(1, 0).UTC(), DeviceID: "a"})
	tomb, _ := event.NewHistoryTombstoned("b", 1, event.HistoryTombstoned{HistoryID: "h", OriginDeviceID: "a", OriginSeq: 1})

	apply := func(order []event.Event) int {
		tx := mk(t)
		defer tx.Rollback()
		for _, ev := range order {
			if err := Apply(tx, ev); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return 0
	}
	_ = apply([]event.Event{created, tomb})
	_ = apply([]event.Event{tomb, created})
}
