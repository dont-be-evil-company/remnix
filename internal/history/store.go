package history

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/mistweaverco/syncsh/internal/db"
)

type Store struct {
	db *sql.DB
}

func NewStore(d *db.DB) *Store {
	return &Store{db: d.SQL}
}

func (s *Store) Insert(e Entry) (bool, error) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	res, err := s.db.Exec(`
INSERT OR IGNORE INTO history (
    id, command, start_ts, end_ts, duration_ms, exit_status, cwd, session_id,
    hostname, device_id, shell, deleted, origin_device_id, origin_seq, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Command, e.StartTS.UnixMilli(), unixMilliPtr(e.EndTS), e.DurationMs, e.ExitStatus,
		nullString(e.Cwd), nullString(e.SessionID), nullString(e.Hostname), e.DeviceID, nullString(e.Shell),
		boolToInt(e.Deleted), nullString(e.OriginDeviceID), e.OriginSeq, e.CreatedAt.UnixMilli(),
	)
	if err != nil {
		return false, fmt.Errorf("insert history: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *Store) Get(id string) (Entry, bool, error) {
	row := s.db.QueryRow(`
SELECT id, command, start_ts, end_ts, duration_ms, exit_status, cwd, session_id,
       hostname, device_id, shell, deleted, origin_device_id, origin_seq, created_at
FROM history WHERE id = ?`, id)
	e, err := scanEntry(row)
	if err == sql.ErrNoRows {
		return Entry{}, false, nil
	}
	if err != nil {
		return Entry{}, false, err
	}
	return e, true, nil
}

func (s *Store) Complete(id string, end time.Time, exitStatus int) error {
	duration := end.UnixMilli()
	_, err := s.db.Exec(`
UPDATE history
SET end_ts = ?,
    exit_status = ?,
    duration_ms = CASE WHEN start_ts IS NULL THEN NULL ELSE (? - start_ts) END
WHERE id = ?`, end.UnixMilli(), exitStatus, duration, id)
	return err
}

func (s *Store) Tombstone(id string) error {
	_, err := s.db.Exec(`UPDATE history SET deleted = 1 WHERE id = ?`, id)
	return err
}

func (s *Store) TombstoneByOrigin(deviceID string, seq int64) error {
	_, err := s.db.Exec(`UPDATE history SET deleted = 1 WHERE origin_device_id = ? AND origin_seq = ?`, deviceID, seq)
	return err
}

type Filter struct {
	Query          string
	Cwd            string
	Host           string
	Shell          string
	Session        string
	DeviceID       string
	Exit           *int
	Since          *time.Time
	Until          *time.Time
	IncludeDeleted bool
	Unique         bool
	Limit          int
}

var historyColumnNames = []string{
	"id", "command", "start_ts", "end_ts", "duration_ms", "exit_status", "cwd", "session_id",
	"hostname", "device_id", "shell", "deleted", "origin_device_id", "origin_seq", "created_at",
}

func historySelect(alias string) string {
	if alias == "" {
		return strings.Join(historyColumnNames, ", ")
	}
	out := make([]string, len(historyColumnNames))
	for i, c := range historyColumnNames {
		out[i] = alias + "." + c
	}
	return strings.Join(out, ", ")
}

func (s *Store) List(f Filter) ([]Entry, error) {
	var b strings.Builder
	var args []any
	if f.Unique {
		predH, argsH := f.predicates("h")
		predH2, argsH2 := f.predicates("h2")
		b.WriteString("SELECT ")
		b.WriteString(historySelect("h"))
		b.WriteString(`
FROM history h
WHERE 1=1`)
		b.WriteString(predH)
		b.WriteString(`
AND NOT EXISTS (
  SELECT 1 FROM history h2
  WHERE 1=1`)
		b.WriteString(predH2)
		b.WriteString(`
  AND h2.command = h.command
  AND (h2.start_ts, h2.id) > (h.start_ts, h.id)
)
ORDER BY h.start_ts DESC`)
		args = append(args, argsH...)
		args = append(args, argsH2...)
	} else {
		b.WriteString("SELECT ")
		b.WriteString(historySelect(""))
		b.WriteString(`
FROM history WHERE 1=1`)
		pred, predArgs := f.predicates("")
		b.WriteString(pred)
		args = append(args, predArgs...)
		b.WriteString(` ORDER BY start_ts DESC`)
	}
	if f.Limit > 0 {
		b.WriteString(` LIMIT ?`)
		args = append(args, f.Limit)
	}
	rows, err := s.db.Query(b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

func (s *Store) SuggestPrefix(prefix string) (string, error) {
	if prefix == "" {
		return "", nil
	}
	var command string
	err := s.db.QueryRow(`
SELECT command FROM history
WHERE deleted = 0 AND command LIKE ? ESCAPE '\' AND command != ?
ORDER BY start_ts DESC, id DESC
LIMIT 1`, likePrefix(prefix), prefix).Scan(&command)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return command, err
}

func likePrefix(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s + "%"
}

func (s *Store) ListByCommand(command string) ([]Entry, error) {
	rows, err := s.db.Query(`SELECT `+historySelect("")+`
FROM history WHERE deleted = 0 AND command = ?
ORDER BY start_ts DESC, id DESC`, command)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	var out []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (f Filter) predicates(alias string) (string, []any) {
	col := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	var b strings.Builder
	var args []any
	if !f.IncludeDeleted {
		b.WriteString(" AND " + col("deleted") + " = 0")
	}
	if f.Cwd != "" {
		b.WriteString(" AND " + col("cwd") + " = ?")
		args = append(args, f.Cwd)
	}
	if f.Host != "" {
		b.WriteString(" AND " + col("hostname") + " = ?")
		args = append(args, f.Host)
	}
	if f.Shell != "" {
		b.WriteString(" AND " + col("shell") + " = ?")
		args = append(args, f.Shell)
	}
	if f.Session != "" {
		b.WriteString(" AND " + col("session_id") + " = ?")
		args = append(args, f.Session)
	}
	if f.DeviceID != "" {
		b.WriteString(" AND " + col("device_id") + " = ?")
		args = append(args, f.DeviceID)
	}
	if f.Exit != nil {
		b.WriteString(" AND " + col("exit_status") + " = ?")
		args = append(args, *f.Exit)
	}
	if f.Since != nil {
		b.WriteString(" AND " + col("start_ts") + " >= ?")
		args = append(args, f.Since.UnixMilli())
	}
	if f.Until != nil {
		b.WriteString(" AND " + col("start_ts") + " <= ?")
		args = append(args, f.Until.UnixMilli())
	}
	if f.Query != "" {
		b.WriteString(" AND instr(lower(" + col("command") + "), lower(?)) > 0")
		args = append(args, f.Query)
	}
	return b.String(), args
}

func (s *Store) Count() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM history WHERE deleted = 0`).Scan(&n)
	return n, err
}

func (s *Store) Stats() (Stats, error) {
	var st Stats
	err := s.db.QueryRow(`
SELECT
    COUNT(*) FILTER (WHERE deleted = 0),
    COUNT(*) FILTER (WHERE deleted = 1),
    COUNT(DISTINCT device_id),
    COUNT(DISTINCT session_id)
FROM history`).Scan(&st.Commands, &st.Deleted, &st.Devices, &st.Sessions)
	if err != nil {
		return st, err
	}
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM history WHERE deleted = 0 AND exit_status = 0`).Scan(&st.Success)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM history WHERE deleted = 0 AND exit_status IS NOT NULL AND exit_status != 0`).Scan(&st.Failed)
	return st, nil
}

type Stats struct {
	Commands int64
	Deleted  int64
	Devices  int64
	Sessions int64
	Success  int64
	Failed   int64
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntry(row rowScanner) (Entry, error) {
	var (
		e         Entry
		startMS   int64
		endMS     sql.NullInt64
		dur       sql.NullInt64
		exit      sql.NullInt64
		cwd       sql.NullString
		session   sql.NullString
		host      sql.NullString
		shell     sql.NullString
		deleted   int
		originDev sql.NullString
		originSeq sql.NullInt64
		createdMS int64
	)
	err := row.Scan(
		&e.ID, &e.Command, &startMS, &endMS, &dur, &exit, &cwd, &session,
		&host, &e.DeviceID, &shell, &deleted, &originDev, &originSeq, &createdMS,
	)
	if err != nil {
		return Entry{}, err
	}
	e.StartTS = time.UnixMilli(startMS).UTC()
	e.CreatedAt = time.UnixMilli(createdMS).UTC()
	if endMS.Valid {
		t := time.UnixMilli(endMS.Int64).UTC()
		e.EndTS = &t
	}
	if dur.Valid {
		v := dur.Int64
		e.DurationMs = &v
	}
	if exit.Valid {
		v := int(exit.Int64)
		e.ExitStatus = &v
	}
	e.Cwd = cwd.String
	e.SessionID = session.String
	e.Hostname = host.String
	e.Shell = shell.String
	e.Deleted = deleted != 0
	e.OriginDeviceID = originDev.String
	if originSeq.Valid {
		v := originSeq.Int64
		e.OriginSeq = &v
	}
	return e, nil
}

func unixMilliPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
