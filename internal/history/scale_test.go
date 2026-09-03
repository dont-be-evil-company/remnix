package history_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
	"github.com/mistweaverco/syncsh/internal/db"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/search"
	"github.com/mistweaverco/syncsh/internal/sync/bundle"
	"github.com/mistweaverco/syncsh/internal/sync/checkpoint"
	"github.com/mistweaverco/syncsh/internal/sync/event"
)

// TestScaleMillionUniqueCommands times Ctrl+R unique search, ghost-text /
// LSP-style suggest menu, and sync encrypt against unique commands.
//
// Skipped by default (too heavy for CI). Run:
//
//	SYNCSH_SCALE=1 go test ./internal/history/ -run TestScaleMillionUniqueCommands -timeout 45m -v
//
// Optional: SYNCSH_SCALE_N=10000 for a shorter dry run (default 1000000).
func TestScaleMillionUniqueCommands(t *testing.T) {
	n := scaleN(t)
	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	sqlDB := d.SQL
	store := history.NewStore(d)
	const (
		deviceID = "dev0"
		prefix   = "git st"
		cwd      = "/work/0"
	)

	t.Logf("seeding %d unique commands", n)
	if elapsed := timed(t, "seed insert", func() error { return history.SeedUnique(sqlDB, n, deviceID) }); elapsed > 0 {
		t.Logf("  %.0f rows/s", float64(n)/elapsed.Seconds())
	}

	var unique []history.Entry
	timed(t, "ctrl+r unique list (limit 5000)", func() error {
		var err error
		unique, err = store.List(history.Filter{Unique: true, Limit: 5000})
		return err
	})
	if len(unique) == 0 {
		t.Fatal("ctrl+r list returned no rows")
	}
	seen := map[string]struct{}{}
	for _, e := range unique {
		if _, ok := seen[e.Command]; ok {
			t.Fatalf("ctrl+r unique list duplicated %q", e.Command)
		}
		seen[e.Command] = struct{}{}
	}
	timed(t, "ctrl+r rank fuzzy query", func() error {
		got := search.RankWith(prefix, unique, false, search.Context{
			Cwd:      cwd,
			DeviceID: deviceID,
		})
		if len(got) == 0 {
			return fmt.Errorf("no ranked hits for %q", prefix)
		}
		return nil
	})
	timed(t, "ctrl+r unique list + cwd", func() error {
		got, err := store.List(history.Filter{Unique: true, Cwd: cwd, Limit: 5000})
		if err != nil {
			return err
		}
		if len(got) == 0 {
			return fmt.Errorf("cwd filter %q returned no rows", cwd)
		}
		for _, e := range got {
			if e.Cwd != cwd {
				return fmt.Errorf("cwd filter leaked %q", e.Cwd)
			}
		}
		return nil
	})

	var cands []history.Entry
	timed(t, "suggest SQL prefix (ghost+menu)", func() error {
		var err error
		cands, err = store.SuggestPrefixCandidates(prefix, 64)
		return err
	})
	if len(cands) == 0 {
		t.Fatalf("suggest candidates empty for %q", prefix)
	}
	timed(t, "ghost-text BestSuggestion", func() error {
		got := search.BestSuggestion(prefix, cands, search.Context{Cwd: cwd, DeviceID: deviceID})
		if got == "" {
			return fmt.Errorf("empty ghost text")
		}
		return nil
	})
	menuMax := config.Suggest{}.MenuLimit()
	timed(t, "lsp-style Suggestions menu", func() error {
		got := search.Suggestions(prefix, cands, search.Context{Cwd: cwd, DeviceID: deviceID}, menuMax)
		if len(got) == 0 {
			return fmt.Errorf("empty suggest menu")
		}
		return nil
	})
	timed(t, "ghost-text end-to-end", func() error {
		cands, err := store.SuggestPrefixCandidates(prefix, 64)
		if err != nil {
			return err
		}
		if search.BestSuggestion(prefix, cands, search.Context{Cwd: cwd, DeviceID: deviceID}) == "" {
			return fmt.Errorf("empty ghost text")
		}
		return nil
	})
	timed(t, "lsp-style menu end-to-end", func() error {
		cands, err := store.SuggestPrefixCandidates(prefix, 64)
		if err != nil {
			return err
		}
		if len(search.Suggestions(prefix, cands, search.Context{Cwd: cwd, DeviceID: deviceID}, menuMax)) == 0 {
			return fmt.Errorf("empty suggest menu")
		}
		return nil
	})
	timed(t, "suggest miss (full scan)", func() error {
		got, err := store.SuggestPrefixCandidates("zzz-no-such-prefix", 64)
		if err != nil {
			return err
		}
		if len(got) != 0 {
			return fmt.Errorf("expected no hits, got %d", len(got))
		}
		return nil
	})

	var all []history.Entry
	timed(t, "load all rows for checkpoint", func() error {
		var err error
		all, err = store.List(history.Filter{IncludeDeleted: true})
		return err
	})
	if len(all) != n {
		t.Fatalf("loaded %d rows, want %d", len(all), n)
	}
	smk, err := envelope.GenerateSMK()
	if err != nil {
		t.Fatal(err)
	}
	timed(t, "sync encrypt checkpoint snapshot", func() error {
		nonce, ct, err := checkpoint.PackSnapshot(all, smk)
		if err != nil {
			return err
		}
		t.Logf("  snapshot ciphertext %s", db.FormatBytes(int64(len(ct))))
		got, err := checkpoint.UnpackSnapshot(smk, nonce, ct)
		if err != nil {
			return err
		}
		if len(got) != n || got[0].Command != all[0].Command || got[n-1].Command != all[n-1].Command {
			return fmt.Errorf("snapshot round-trip mismatch len=%d", len(got))
		}
		return nil
	})

	evs := make([]event.Event, n)
	timed(t, "encode history-created events", func() error {
		for i, e := range all {
			seq := int64(i + 1)
			if e.OriginSeq != nil {
				seq = *e.OriginSeq
			}
			ev, err := event.NewHistoryCreated(deviceID, seq, e)
			if err != nil {
				return err
			}
			evs[i] = ev
		}
		return nil
	})
	timed(t, "sync encrypt event bundle", func() error {
		raw, h, err := bundle.Pack(deviceID, "gen-scale", evs, smk)
		if err != nil {
			return err
		}
		t.Logf("  bundle %s events=%d", db.FormatBytes(int64(len(raw))), h.EventCount)
		_, got, err := bundle.Unpack(raw, smk)
		if err != nil {
			return err
		}
		if len(got) != n {
			return fmt.Errorf("unpacked %d events", len(got))
		}
		return nil
	})
}

func scaleN(t testing.TB) int {
	t.Helper()
	if testing.Short() {
		t.Skip("scale test skipped with -short")
	}
	if os.Getenv("SYNCSH_SCALE") == "" {
		t.Skip("set SYNCSH_SCALE=1 to time 1M unique commands (optional SYNCSH_SCALE_N)")
	}
	n := 1_000_000
	if s := os.Getenv("SYNCSH_SCALE_N"); s != "" {
		v, err := strconv.Atoi(s)
		if err != nil || v <= 0 {
			t.Fatalf("SYNCSH_SCALE_N=%q", s)
		}
		n = v
	}
	return n
}

func timed(t *testing.T, name string, fn func() error) time.Duration {
	t.Helper()
	start := time.Now()
	if err := fn(); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	d := time.Since(start)
	t.Logf("%-36s %s", name, d)
	return d
}
