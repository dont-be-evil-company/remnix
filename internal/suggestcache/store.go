package suggestcache

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/db"
	"github.com/dont-be-evil-company/remnix/internal/helpparse"
	"github.com/dont-be-evil-company/remnix/internal/suggestcache/migrations"
)

var _ helpparse.Durable = (*Store)(nil)

// Store is the persistent L2 CLI help cache (suggest-cache.db).
type Store struct {
	db *db.DB
}

// Tool is metadata for one CLI binary in the cache.
type Tool struct {
	Name     string
	BinPath  string
	BinMtime int64
	BinSize  int64
	Version  string
	WarmedAt int64
}

// Open opens path and applies suggest-cache migrations.
func Open(path string) (*Store, error) {
	d, err := db.Open(path)
	if err != nil {
		return nil, err
	}
	if err := db.MigrateFS(d.SQL, migrations.FS); err != nil {
		_ = d.Close()
		return nil, err
	}
	return &Store{db: d}, nil
}

// Close checkpoints WAL and closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Load implements helpparse.Durable.
func (s *Store) Load(argv []string) (ents []helpparse.Entity, summary string, ok bool) {
	if s == nil || s.db == nil || len(argv) == 0 {
		return nil, "", false
	}
	pages := s.LoadMany([][]string{argv})
	page, ok := pages[helpparse.ArgvKey(argv)]
	if !ok {
		return nil, "", false
	}
	return page.Entities, page.Summary, true
}

// LoadMany implements helpparse.Durable.
func (s *Store) LoadMany(argvs [][]string) map[string]helpparse.DurablePage {
	out := make(map[string]helpparse.DurablePage)
	if s == nil || s.db == nil || len(argvs) == 0 {
		return out
	}
	keys := uniqueKeys(argvs)
	if len(keys) == 0 {
		return out
	}
	ph := placeholders(len(keys))
	args := asAny(keys)
	rows, err := s.db.SQL.Query(`SELECT argv, summary FROM pages WHERE argv IN (`+ph+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var argv, summary string
		if err := rows.Scan(&argv, &summary); err != nil {
			return out
		}
		out[argv] = helpparse.DurablePage{Summary: summary}
	}
	if err := rows.Err(); err != nil {
		return out
	}
	if len(out) == 0 {
		return out
	}
	pageKeys := make([]string, 0, len(out))
	for k := range out {
		pageKeys = append(pageKeys, k)
	}
	erows, err := s.db.SQL.Query(
		`SELECT parent_argv, name, kind, descr FROM entities WHERE parent_argv IN (`+placeholders(len(pageKeys))+`) ORDER BY name`,
		asAny(pageKeys)...,
	)
	if err != nil {
		return out
	}
	defer erows.Close()
	for erows.Next() {
		var parent, name, kind, descr string
		if err := erows.Scan(&parent, &name, &kind, &descr); err != nil {
			return out
		}
		page := out[parent]
		page.Entities = append(page.Entities, helpparse.Entity{
			Name:  name,
			Descr: descr,
			Kind:  helpparse.Kind(kind),
		})
		out[parent] = page
	}
	_ = erows.Err()
	return out
}

// Save implements helpparse.Durable. Empty ents/summary still count as a hit.
func (s *Store) Save(argv []string, ents []helpparse.Entity, summary string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("suggest cache: closed")
	}
	if len(argv) == 0 {
		return fmt.Errorf("suggest cache: empty argv")
	}
	key := helpparse.ArgvKey(argv)
	tool := ToolName(argv)
	tx, err := s.db.SQL.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO tools (name) VALUES (?) ON CONFLICT(name) DO NOTHING`,
		tool,
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO pages (argv, tool, summary, probed_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(argv) DO UPDATE SET summary=excluded.summary, probed_at=excluded.probed_at, tool=excluded.tool`,
		key, tool, summary, time.Now().Unix(),
	); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM entities WHERE parent_argv = ?`, key); err != nil {
		_ = tx.Rollback()
		return err
	}
	seen := map[string]struct{}{}
	for _, e := range ents {
		name := strings.TrimSpace(e.Name)
		if name == "" {
			continue
		}
		kind := e.Kind
		if kind == "" {
			if strings.HasPrefix(name, "-") {
				kind = helpparse.KindFlag
			} else {
				kind = helpparse.KindCommand
			}
		}
		dup := name + "\x00" + string(kind)
		if _, ok := seen[dup]; ok {
			continue
		}
		seen[dup] = struct{}{}
		if _, err := tx.Exec(
			`INSERT INTO entities (parent_argv, name, kind, descr) VALUES (?, ?, ?, ?)`,
			key, name, string(kind), e.Descr,
		); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Purge deletes cached pages (and cascading entities) for the named tools.
func (s *Store) Purge(tools ...string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("suggest cache: closed")
	}
	if len(tools) == 0 {
		return fmt.Errorf("suggest cache: no tools")
	}
	tx, err := s.db.SQL.Begin()
	if err != nil {
		return err
	}
	for _, tool := range tools {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM pages WHERE tool = ?`, tool); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.Exec(`DELETE FROM tools WHERE name = ?`, tool); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// PurgeAll wipes the entire suggest cache.
func (s *Store) PurgeAll() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("suggest cache: closed")
	}
	tx, err := s.db.SQL.Begin()
	if err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM entities`,
		`DELETE FROM pages`,
		`DELETE FROM tools`,
	} {
		if _, err := tx.Exec(q); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// UpsertTool records binary metadata used by warmup.
func (s *Store) UpsertTool(t Tool) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("suggest cache: closed")
	}
	if t.Name == "" {
		return fmt.Errorf("suggest cache: empty tool name")
	}
	_, err := s.db.SQL.Exec(
		`INSERT INTO tools (name, bin_path, bin_mtime, bin_size, version, warmed_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(name) DO UPDATE SET
		   bin_path=excluded.bin_path,
		   bin_mtime=excluded.bin_mtime,
		   bin_size=excluded.bin_size,
		   version=excluded.version,
		   warmed_at=excluded.warmed_at`,
		t.Name, t.BinPath, t.BinMtime, t.BinSize, t.Version, t.WarmedAt,
	)
	return err
}

// ToolName is the cache tool key for argv (basename of argv[0]).
func ToolName(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	return filepath.Base(argv[0])
}

func uniqueKeys(argvs [][]string) []string {
	seen := map[string]struct{}{}
	var keys []string
	for _, a := range argvs {
		if len(a) == 0 {
			continue
		}
		k := helpparse.ArgvKey(a)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		keys = append(keys, k)
	}
	return keys
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

func asAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
