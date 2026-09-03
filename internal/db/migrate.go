package db

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/mistweaverco/syncsh/internal/db/migrations"
)

type Migration struct {
	ID       string
	SQL      string
	Checksum string
}

type Status struct {
	ID        string
	Checksum  string
	Applied   bool
	AppliedAt *time.Time
	Mismatch  bool
}

func LoadMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	out := make([]Migration, 0, len(names))
	for _, name := range names {
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		sum := sha256.Sum256(body)
		id := strings.TrimSuffix(name, ".sql")
		out = append(out, Migration{
			ID:       id,
			SQL:      string(body),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	return out, nil
}

func Migrate(sqlDB *sql.DB) error {
	if err := ensureMigrationsTable(sqlDB); err != nil {
		return err
	}
	migs, err := LoadMigrations()
	if err != nil {
		return err
	}
	applied, err := appliedMigrations(sqlDB)
	if err != nil {
		return err
	}
	for _, m := range migs {
		if rec, ok := applied[m.ID]; ok {
			if rec.checksum != m.Checksum {
				return fmt.Errorf("migration %s checksum mismatch: stored %s, embedded %s", m.ID, rec.checksum, m.Checksum)
			}
			continue
		}
		tx, err := sqlDB.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", m.ID, err)
		}
		for _, stmt := range splitSQL(m.SQL) {
			if _, err := tx.Exec(stmt); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply migration %s: %w", m.ID, err)
			}
		}
		if hook := postHooks[m.ID]; hook != nil {
			if err := hook(tx); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("apply migration %s: %w", m.ID, err)
			}
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (id, checksum, applied_at) VALUES (?, ?, ?)`,
			m.ID, m.Checksum, time.Now().Unix(),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", m.ID, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.ID, err)
		}
	}
	return nil
}

func MigrationStatus(sqlDB *sql.DB) ([]Status, error) {
	if err := ensureMigrationsTable(sqlDB); err != nil {
		return nil, err
	}
	migs, err := LoadMigrations()
	if err != nil {
		return nil, err
	}
	applied, err := appliedMigrations(sqlDB)
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(migs))
	for _, m := range migs {
		st := Status{ID: m.ID, Checksum: m.Checksum}
		if rec, ok := applied[m.ID]; ok {
			st.Applied = true
			t := time.Unix(rec.appliedAt, 0).UTC()
			st.AppliedAt = &t
			st.Mismatch = rec.checksum != m.Checksum
		}
		out = append(out, st)
	}
	return out, nil
}

type appliedRec struct {
	checksum  string
	appliedAt int64
}

func ensureMigrationsTable(sqlDB *sql.DB) error {
	_, err := sqlDB.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
    id TEXT PRIMARY KEY,
    checksum TEXT NOT NULL,
    applied_at INTEGER NOT NULL
)`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	return nil
}

func splitSQL(s string) []string {
	var stmts []string
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "--") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
		if strings.HasSuffix(strings.TrimSpace(b.String()), ";") {
			stmt := strings.TrimSpace(b.String())
			if stmt != "" && stmt != ";" {
				stmts = append(stmts, stmt)
			}
			b.Reset()
		}
	}
	if rest := strings.TrimSpace(b.String()); rest != "" {
		stmts = append(stmts, rest)
	}
	return stmts
}

func appliedMigrations(sqlDB *sql.DB) (map[string]appliedRec, error) {
	rows, err := sqlDB.Query(`SELECT id, checksum, applied_at FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()
	out := map[string]appliedRec{}
	for rows.Next() {
		var id, checksum string
		var appliedAt int64
		if err := rows.Scan(&id, &checksum, &appliedAt); err != nil {
			return nil, err
		}
		out[id] = appliedRec{checksum: checksum, appliedAt: appliedAt}
	}
	return out, rows.Err()
}
