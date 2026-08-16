package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type DB struct {
	SQL *sql.DB
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(4)
	if _, err := sqlDB.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	if _, err := sqlDB.Exec(`PRAGMA synchronous=NORMAL`); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("set synchronous: %w", err)
	}
	return &DB{SQL: sqlDB}, nil
}

func OpenAndMigrate(path string) (*DB, error) {
	d, err := Open(path)
	if err != nil {
		return nil, err
	}
	if err := Migrate(d.SQL); err != nil {
		_ = d.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error {
	if d == nil || d.SQL == nil {
		return nil
	}
	return d.SQL.Close()
}

func IntegrityCheck(sqlDB *sql.DB) (string, error) {
	var result string
	if err := sqlDB.QueryRow(`PRAGMA integrity_check`).Scan(&result); err != nil {
		return "", err
	}
	return result, nil
}
