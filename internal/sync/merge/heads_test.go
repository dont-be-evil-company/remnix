package merge

import (
	"path/filepath"
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/db"
)

func TestHeadStoreSetIsMonotonic(t *testing.T) {
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := NewHeadStore(d)
	if err := s.Set("dev", 50); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("dev", 20); err != nil {
		t.Fatal(err)
	}
	f, err := s.Get()
	if err != nil {
		t.Fatal(err)
	}
	if f["dev"] != 50 {
		t.Fatalf("head rewound: %d", f["dev"])
	}
	if err := s.Set("dev", 51); err != nil {
		t.Fatal(err)
	}
	f, _ = s.Get()
	if f["dev"] != 51 {
		t.Fatalf("head did not advance: %d", f["dev"])
	}
}
