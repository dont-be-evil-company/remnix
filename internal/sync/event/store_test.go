package event

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/db"
	"github.com/dont-be-evil-company/remnix/internal/history"
)

func TestAppendStripsHistoryPayload(t *testing.T) {
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hs := history.NewStore(d)
	e := history.Entry{ID: "h1", Command: "ls", StartTS: time.Unix(1, 0).UTC(), DeviceID: "dev", Cwd: "/tmp"}
	if _, err := hs.Insert(e); err != nil {
		t.Fatal(err)
	}
	seq := int64(1)
	e.OriginDeviceID = "dev"
	e.OriginSeq = &seq
	if _, err := d.SQL.Exec(`UPDATE history SET origin_device_id = ?, origin_seq = ? WHERE id = ?`, "dev", seq, e.ID); err != nil {
		t.Fatal(err)
	}
	store := NewStore(d)
	ev, err := NewHistoryCreated("dev", seq, e)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ev); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkApplied("dev", seq); err != nil {
		t.Fatal(err)
	}
	var payload []byte
	var historyID string
	if err := d.SQL.QueryRow(`SELECT payload, history_id FROM sync_events WHERE device_id=? AND seq=?`, "dev", seq).Scan(&payload, &historyID); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 0 {
		t.Fatalf("payload still stored: %d bytes", len(payload))
	}
	if historyID != "h1" {
		t.Fatalf("history_id %q", historyID)
	}
	got, ok, err := store.Get("dev", seq)
	if err != nil || !ok {
		t.Fatalf("get ok=%v err=%v", ok, err)
	}
	p, err := DecodeHistoryCreated(got)
	if err != nil {
		t.Fatal(err)
	}
	if p.Command != "ls" || p.Cwd != "/tmp" {
		t.Fatalf("%+v", p)
	}
}

func TestRangeReconstructsCompactedCreated(t *testing.T) {
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hs := history.NewStore(d)
	store := NewStore(d)
	for i, cmd := range []string{"one", "two", "three"} {
		seq := int64(i + 1)
		e := history.Entry{
			ID:             "h" + cmd,
			Command:        cmd,
			StartTS:        time.Unix(int64(i+1), 0).UTC(),
			DeviceID:       "dev",
			OriginDeviceID: "dev",
			OriginSeq:      &seq,
		}
		if _, err := hs.Insert(e); err != nil {
			t.Fatal(err)
		}
		ev, err := NewHistoryCreated("dev", seq, e)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Append(ev); err != nil {
			t.Fatal(err)
		}
		if err := store.MarkApplied("dev", seq); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.SQL.Exec(`INSERT INTO sync_heads (device_id, seq) VALUES ('dev', 3)`); err != nil {
		t.Fatal(err)
	}
	if err := store.CompactUpToHeads(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.SQL.QueryRow(`SELECT COUNT(*) FROM sync_events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("created events not compacted: %d", n)
	}
	got, err := store.Range("dev", 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("range %d", len(got))
	}
	p, _ := DecodeHistoryCreated(got[1])
	if p.Command != "two" {
		t.Fatalf("%+v", p)
	}
}
