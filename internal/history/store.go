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

func NewSQLStore(sqlDB *sql.DB) *Store {
	return &Store{db: sqlDB}
}

func (s *Store) Insert(e Entry) (bool, error) {
	return InsertExec(s.db, e)
}

func InsertTx(tx *sql.Tx, e Entry) (bool, error) {
	return InsertExec(tx, e)
}

func InsertExec(eq dbExec, e Entry) (bool, error) {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	cwdID, err := internID(eq, "intern_cwd", e.Cwd)
	if err != nil {
		return false, err
	}
	sessID, err := internID(eq, "intern_session", e.SessionID)
	if err != nil {
		return false, err
	}
	hostID, err := internID(eq, "intern_hostname", e.Hostname)
	if err != nil {
		return false, err
	}
	shellID, err := internID(eq, "intern_shell", e.Shell)
	if err != nil {
		return false, err
	}
	res, err := eq.Exec(`
INSERT OR IGNORE INTO history (
    id, command, command_hash, start_ts, end_ts, duration_ms, exit_status, cwd_id, session_id,
    hostname_id, device_id, shell_id, deleted, origin_device_id, origin_seq, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.Command, HashCommand(e.Command), e.StartTS.UnixMilli(), unixMilliPtr(e.EndTS), e.DurationMs, e.ExitStatus,
		cwdID, sessID, hostID, e.DeviceID, shellID,
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

func historySelect(alias string) string {
	p := alias
	if p == "" {
		p = "h"
	}
	return strings.Join([]string{
		p + ".id",
		p + ".command",
		p + ".start_ts",
		p + ".end_ts",
		p + ".duration_ms",
		p + ".exit_status",
		p + "_cwd.value",
		p + "_session.value",
		p + "_host.value",
		p + ".device_id",
		p + "_shell.value",
		p + ".deleted",
		p + ".origin_device_id",
		p + ".origin_seq",
		p + ".created_at",
	}, ", ")
}

func historyFrom(alias string) string {
	p := alias
	if p == "" {
		p = "h"
	}
	return `history ` + p + `
LEFT JOIN intern_cwd ` + p + `_cwd ON ` + p + `_cwd.id = ` + p + `.cwd_id
LEFT JOIN intern_session ` + p + `_session ON ` + p + `_session.id = ` + p + `.session_id
LEFT JOIN intern_hostname ` + p + `_host ON ` + p + `_host.id = ` + p + `.hostname_id
LEFT JOIN intern_shell ` + p + `_shell ON ` + p + `_shell.id = ` + p + `.shell_id`
}

func (s *Store) Get(id string) (Entry, bool, error) {
	row := s.db.QueryRow(`SELECT `+historySelect("h")+` FROM `+historyFrom("h")+` WHERE h.id = ?`, id)
	return scanFound(row)
}

func (s *Store) ByOrigin(deviceID string, seq int64) (Entry, bool, error) {
	row := s.db.QueryRow(`SELECT `+historySelect("h")+` FROM `+historyFrom("h")+`
WHERE h.origin_device_id = ? AND h.origin_seq = ?`, deviceID, seq)
	return scanFound(row)
}

func (s *Store) ListByOriginRange(deviceID string, start, end int64) ([]Entry, error) {
	rows, err := s.db.Query(`SELECT `+historySelect("h")+` FROM `+historyFrom("h")+`
WHERE h.origin_device_id = ? AND h.origin_seq >= ? AND h.origin_seq <= ?
ORDER BY h.origin_seq`, deviceID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

func scanFound(row *sql.Row) (Entry, bool, error) {
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

func (s *Store) List(f Filter) ([]Entry, error) {
	var b strings.Builder
	var args []any
	if f.Unique {
		predH, argsH := f.predicates("h")
		predH2, argsH2 := f.predicates("h2")
		b.WriteString("SELECT ")
		b.WriteString(historySelect("h"))
		b.WriteString("\nFROM ")
		b.WriteString(historyFrom("h"))
		b.WriteString("\nWHERE 1=1")
		b.WriteString(predH)
		b.WriteString(`
AND NOT EXISTS (
  SELECT 1 FROM `)
		b.WriteString(historyFrom("h2"))
		b.WriteString(`
  WHERE 1=1`)
		b.WriteString(predH2)
		b.WriteString(`
  AND h2.command_hash = h.command_hash
  AND (h2.start_ts, h2.id) > (h.start_ts, h.id)
)
ORDER BY h.start_ts DESC`)
		args = append(args, argsH...)
		args = append(args, argsH2...)
	} else {
		b.WriteString("SELECT ")
		b.WriteString(historySelect("h"))
		b.WriteString("\nFROM ")
		b.WriteString(historyFrom("h"))
		b.WriteString("\nWHERE 1=1")
		pred, predArgs := f.predicates("h")
		b.WriteString(pred)
		args = append(args, predArgs...)
		b.WriteString(` ORDER BY h.start_ts DESC`)
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
	cands, err := s.SuggestPrefixCandidates(prefix, 32)
	if err != nil || len(cands) == 0 {
		return "", err
	}
	return cands[0].Command, nil
}

func (s *Store) SuggestPrefixCandidates(prefix string, limit int) ([]Entry, error) {
	if prefix == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 32
	}
	rows, err := s.db.Query(`
SELECT `+historySelect("h")+`
FROM `+historyFrom("h")+`
WHERE h.deleted = 0 AND h.command LIKE ? ESCAPE '\' AND h.command != ?
ORDER BY h.start_ts DESC, h.id DESC
LIMIT ?`, likePrefix(prefix), prefix, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

func likePrefix(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s + "%"
}

func (s *Store) ListByCommand(command string) ([]Entry, error) {
	rows, err := s.db.Query(`SELECT `+historySelect("h")+`
FROM `+historyFrom("h")+` WHERE h.deleted = 0 AND h.command = ?
ORDER BY h.start_ts DESC, h.id DESC`, command)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

type SummarySort int

const (
	SortRecent SummarySort = iota
	SortTop
	SortFailed
)

func (s SummarySort) Next() SummarySort {
	return (s + 1) % 3
}

type CommandSummary struct {
	Command        string
	Runs           int64
	Success        int64
	Failed         int64
	FirstTS        time.Time
	LastTS         time.Time
	LastExit       *int
	LastCwd        string
	LastHost       string
	LastDurationMs *int64
}

type SummaryFilter struct {
	Query string
	Sort  SummarySort
	Limit int
}

func (s *Store) CommandSummaries(f SummaryFilter) ([]CommandSummary, error) {
	var b strings.Builder
	var args []any
	b.WriteString(`
SELECT
    h.command,
    agg.runs,
    agg.success,
    agg.failed,
    agg.first_ts,
    agg.last_ts,
    h.exit_status,
    h_cwd.value,
    h_host.value,
    h.duration_ms
FROM `)
	b.WriteString(historyFrom("h"))
	b.WriteString(`
INNER JOIN (
    SELECT
        command_hash,
        COUNT(*) AS runs,
        COUNT(*) FILTER (WHERE exit_status = 0) AS success,
        COUNT(*) FILTER (WHERE exit_status IS NOT NULL AND exit_status != 0) AS failed,
        MIN(start_ts) AS first_ts,
        MAX(start_ts) AS last_ts
    FROM history
    WHERE deleted = 0`)
	if f.Query != "" {
		b.WriteString(`
    AND instr(lower(command), lower(?)) > 0`)
		args = append(args, f.Query)
	}
	b.WriteString(`
    GROUP BY command_hash`)
	if f.Sort == SortFailed {
		b.WriteString(`
    HAVING COUNT(*) FILTER (WHERE exit_status IS NOT NULL AND exit_status != 0) > 0`)
	}
	b.WriteString(`
) agg ON agg.command_hash = h.command_hash
WHERE h.deleted = 0
AND NOT EXISTS (
    SELECT 1 FROM history h2
    WHERE h2.deleted = 0
    AND h2.command_hash = h.command_hash
    AND (h2.start_ts, h2.id) > (h.start_ts, h.id)
)`)
	switch f.Sort {
	case SortTop:
		b.WriteString(`
ORDER BY agg.runs DESC, agg.last_ts DESC, h.command`)
	default:
		b.WriteString(`
ORDER BY agg.last_ts DESC, h.command`)
	}
	if f.Limit > 0 {
		b.WriteString(`
LIMIT ?`)
		args = append(args, f.Limit)
	}
	rows, err := s.db.Query(b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommandSummary
	for rows.Next() {
		var (
			cs      CommandSummary
			firstMS int64
			lastMS  int64
			exit    sql.NullInt64
			cwd     sql.NullString
			host    sql.NullString
			dur     sql.NullInt64
		)
		if err := rows.Scan(
			&cs.Command, &cs.Runs, &cs.Success, &cs.Failed, &firstMS, &lastMS,
			&exit, &cwd, &host, &dur,
		); err != nil {
			return nil, err
		}
		cs.FirstTS = time.UnixMilli(firstMS).UTC()
		cs.LastTS = time.UnixMilli(lastMS).UTC()
		if exit.Valid {
			v := int(exit.Int64)
			cs.LastExit = &v
		}
		cs.LastCwd = cwd.String
		cs.LastHost = host.String
		if dur.Valid {
			v := dur.Int64
			cs.LastDurationMs = &v
		}
		out = append(out, cs)
	}
	return out, rows.Err()
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
	p := alias
	if p == "" {
		p = "h"
	}
	col := func(name string) string {
		return p + "." + name
	}
	var b strings.Builder
	var args []any
	if !f.IncludeDeleted {
		b.WriteString(" AND " + col("deleted") + " = 0")
	}
	if f.Cwd != "" {
		b.WriteString(" AND " + col("cwd_id") + " = (SELECT id FROM intern_cwd WHERE value = ?)")
		args = append(args, f.Cwd)
	}
	if f.Host != "" {
		b.WriteString(" AND " + col("hostname_id") + " = (SELECT id FROM intern_hostname WHERE value = ?)")
		args = append(args, f.Host)
	}
	if f.Shell != "" {
		b.WriteString(" AND " + col("shell_id") + " = (SELECT id FROM intern_shell WHERE value = ?)")
		args = append(args, f.Shell)
	}
	if f.Session != "" {
		b.WriteString(" AND " + col("session_id") + " = (SELECT id FROM intern_session WHERE value = ?)")
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
