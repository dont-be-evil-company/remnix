package importers

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/dont-be-evil-company/remnix/internal/history"
)

func ImportAtuin(path, deviceID string) ([]history.Entry, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open atuin db: %w", err)
	}
	defer db.Close()

	rows, err := db.Query(`
SELECT id, timestamp, duration, exit, command, cwd, session, hostname
FROM history`)
	if err != nil {
		return nil, fmt.Errorf("query atuin history: %w", err)
	}
	defer rows.Close()

	var out []history.Entry
	for rows.Next() {
		var (
			id, command, cwd, session, hostname string
			ts, duration, exitCode              int64
		)
		if err := rows.Scan(&id, &ts, &duration, &exitCode, &command, &cwd, &session, &hostname); err != nil {
			return nil, err
		}
		if strings.TrimSpace(command) == "" {
			continue
		}
		dur := durationToMs(duration)
		ex := int(exitCode)
		out = append(out, history.Entry{
			ID:         id,
			Command:    command,
			StartTS:    unixMaybeNano(ts),
			DurationMs: &dur,
			ExitStatus: &ex,
			Cwd:        cwd,
			SessionID:  session,
			Hostname:   hostname,
			DeviceID:   deviceID,
			Shell:      "atuin",
		})
	}
	return out, rows.Err()
}

func unixMaybeNano(ts int64) time.Time {
	if ts > 1e12 {
		return time.Unix(0, ts).UTC()
	}
	return time.Unix(ts, 0).UTC()
}

func durationToMs(d int64) int64 {
	if d > 1e6 {
		return d / 1e6
	}
	return d
}
