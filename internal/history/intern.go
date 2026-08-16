package history

import "database/sql"

type dbExec interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func internID(eq dbExec, table, value string) (any, error) {
	if value == "" {
		return nil, nil
	}
	if _, err := eq.Exec(`INSERT OR IGNORE INTO `+table+` (value) VALUES (?)`, value); err != nil {
		return nil, err
	}
	var id int64
	if err := eq.QueryRow(`SELECT id FROM `+table+` WHERE value = ?`, value).Scan(&id); err != nil {
		return nil, err
	}
	return id, nil
}
