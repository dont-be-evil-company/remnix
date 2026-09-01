package event

import (
	"path/filepath"
	"testing"

	"github.com/mistweaverco/syncsh/internal/db"
)

func TestNextSeqSkipsCompactedHeads(t *testing.T) {
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := NewStore(d)
	seq, err := s.NextSeq("dev")
	if err != nil || seq != 1 {
		t.Fatalf("empty: seq=%d err=%v", seq, err)
	}
	if err := s.Append(Event{Version: 1, Type: TypeHistoryCreated, DeviceID: "dev", Seq: 3, TimeUnix: 1}); err != nil {
		t.Fatal(err)
	}
	seq, err = s.NextSeq("dev")
	if err != nil || seq != 4 {
		t.Fatalf("from events: seq=%d err=%v", seq, err)
	}
	if _, err := d.SQL.Exec(`INSERT INTO sync_heads (device_id, seq) VALUES (?, ?)`, "dev", 80); err != nil {
		t.Fatal(err)
	}
	seq, err = s.NextSeq("dev")
	if err != nil || seq != 81 {
		t.Fatalf("from compacted head: seq=%d err=%v", seq, err)
	}
}
