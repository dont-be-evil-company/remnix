package history

import (
	"database/sql"
	"fmt"
	"strconv"
	"time"
)

// SeedUnique inserts n unique commands for scale tests. Intern tables are
// filled first so each row is a single prepared INSERT.
func SeedUnique(sqlDB *sql.DB, n int, deviceID string) error {
	if n <= 0 {
		return fmt.Errorf("n must be positive")
	}
	if _, err := sqlDB.Exec(`PRAGMA synchronous=OFF`); err != nil {
		return err
	}
	if _, err := sqlDB.Exec(`PRAGMA cache_size=-262144`); err != nil {
		return err
	}
	tx, err := sqlDB.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	const nCwd, nSess = 128, 512
	cwdIDs := make([]int64, nCwd)
	for i := range cwdIDs {
		id, err := internID(tx, "intern_cwd", "/work/"+strconv.Itoa(i))
		if err != nil {
			return err
		}
		cwdIDs[i] = id.(int64)
	}
	sessIDs := make([]int64, nSess)
	for i := range sessIDs {
		id, err := internID(tx, "intern_session", "sess-"+strconv.Itoa(i))
		if err != nil {
			return err
		}
		sessIDs[i] = id.(int64)
	}
	hostID, err := internID(tx, "intern_hostname", "scale-host")
	if err != nil {
		return err
	}
	shellID, err := internID(tx, "intern_shell", "zsh")
	if err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
INSERT INTO history (
    id, command, command_hash, start_ts, end_ts, duration_ms, exit_status, cwd_id, session_id,
    hostname_id, device_id, shell_id, deleted, origin_device_id, origin_seq, created_at
) VALUES (?, ?, ?, ?, NULL, NULL, NULL, ?, ?, ?, ?, ?, 0, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	for i := 0; i < n; i++ {
		cmd := "git status --job-" + strconv.Itoa(i)
		seq := int64(i + 1)
		ts := base + int64(i)
		if _, err := stmt.Exec(
			strconv.Itoa(i), cmd, HashCommand(cmd), ts,
			cwdIDs[i%nCwd], sessIDs[i%nSess], hostID, deviceID, shellID,
			deviceID, seq, ts,
		); err != nil {
			return fmt.Errorf("insert %d: %w", i, err)
		}
	}
	return tx.Commit()
}
