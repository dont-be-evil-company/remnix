package history

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AdvancedFilter selects runs with regex and structured predicates ANDed together.
// Empty fields are unconstrained. SQL applies intern/time/exit predicates; CommandRE
// is applied after grouping (command text is constant per command_hash).
type AdvancedFilter struct {
	CommandRE     *regexp.Regexp
	CommandNeedle string
	CwdRE         *regexp.Regexp
	HostRE        *regexp.Regexp
	ShellRE       *regexp.Regexp
	SessionRE     *regexp.Regexp
	DeviceRE      *regexp.Regexp
	ExitRE        *regexp.Regexp
	ExitFailed    bool
	Exit          *int
	Since         *time.Time
	Until         *time.Time
	Sort          SummarySort
	Limit         int
}

func (f AdvancedFilter) Active() bool {
	return f.CommandRE != nil || f.CommandNeedle != "" || f.CwdRE != nil || f.HostRE != nil ||
		f.ShellRE != nil || f.SessionRE != nil || f.DeviceRE != nil || f.ExitRE != nil ||
		f.ExitFailed || f.Exit != nil || f.Since != nil || f.Until != nil
}

// RegexLiteralPrefix returns the longest leading substring of pat that has no
// regexp metacharacters. Used as an SQL instr() prefilter.
func RegexLiteralPrefix(pat string) string {
	if pat == "" {
		return ""
	}
	if regexp.QuoteMeta(pat) == pat {
		return pat
	}
	var b strings.Builder
	for i := 0; i < len(pat); i++ {
		c := pat[i]
		if c == '\\' {
			break
		}
		switch c {
		case '.', '*', '+', '?', '|', '(', ')', '[', ']', '{', '}', '^', '$':
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func (f AdvancedFilter) Match(e Entry) bool {
	if e.Deleted {
		return false
	}
	if f.CommandRE != nil && !f.CommandRE.MatchString(e.Command) {
		return false
	}
	if f.CwdRE != nil && !f.CwdRE.MatchString(e.Cwd) {
		return false
	}
	if f.HostRE != nil && !f.HostRE.MatchString(e.Hostname) {
		return false
	}
	if f.ShellRE != nil && !f.ShellRE.MatchString(e.Shell) {
		return false
	}
	if f.SessionRE != nil && !f.SessionRE.MatchString(e.SessionID) {
		return false
	}
	if f.DeviceRE != nil && !f.DeviceRE.MatchString(e.DeviceID) {
		return false
	}
	if f.ExitFailed {
		if e.ExitStatus == nil || *e.ExitStatus == 0 {
			return false
		}
	}
	if f.Exit != nil {
		if e.ExitStatus == nil || *e.ExitStatus != *f.Exit {
			return false
		}
	}
	if f.ExitRE != nil {
		if e.ExitStatus == nil || !f.ExitRE.MatchString(strconv.Itoa(*e.ExitStatus)) {
			return false
		}
	}
	if f.Since != nil && e.StartTS.Before(*f.Since) {
		return false
	}
	if f.Until != nil && e.StartTS.After(*f.Until) {
		return false
	}
	return true
}

func FilterEntries(in []Entry, f AdvancedFilter) []Entry {
	if !f.Active() {
		out := make([]Entry, 0, len(in))
		for _, e := range in {
			if !e.Deleted {
				out = append(out, e)
			}
		}
		return out
	}
	out := make([]Entry, 0, len(in))
	for _, e := range in {
		if f.Match(e) {
			out = append(out, e)
		}
	}
	return out
}

func SummarizeRuns(command string, runs []Entry) CommandSummary {
	cs := CommandSummary{Command: command}
	if len(runs) == 0 {
		return cs
	}
	var last Entry
	for i, e := range runs {
		cs.Runs++
		if e.ExitStatus != nil {
			if *e.ExitStatus == 0 {
				cs.Success++
			} else {
				cs.Failed++
			}
		}
		if i == 0 || e.StartTS.Before(cs.FirstTS) {
			cs.FirstTS = e.StartTS
		}
		if i == 0 || e.StartTS.After(cs.LastTS) || (e.StartTS.Equal(cs.LastTS) && e.ID > last.ID) {
			cs.LastTS = e.StartTS
			last = e
		}
	}
	cs.LastCwd = last.Cwd
	cs.LastHost = last.Hostname
	cs.LastExit = last.ExitStatus
	cs.LastDurationMs = last.DurationMs
	return cs
}

type resolvedAdvanced struct {
	impossible    bool
	commandNeedle string
	exitFailed    bool
	exit          *int
	since         *time.Time
	until         *time.Time
	cwd           columnMatch
	host          columnMatch
	shell         columnMatch
	session       columnMatch
	device        stringMatch
	exits         intMatch
}

type columnMatch struct {
	set      bool
	ids      []int64
	allowNil bool
	notNil   bool
}

type stringMatch struct {
	set      bool
	vals     []string
	allowNil bool
	notNil   bool
}

type intMatch struct {
	set      bool
	vals     []int
	allowNil bool
}

func (s *Store) CommandSummariesAdvanced(f AdvancedFilter) ([]CommandSummary, error) {
	if f.CommandNeedle == "" && f.CommandRE != nil {
		f.CommandNeedle = RegexLiteralPrefix(f.CommandRE.String())
	}
	if !f.Active() {
		return s.CommandSummaries(SummaryFilter{Sort: f.Sort, Limit: f.Limit})
	}
	r, err := s.resolveAdvanced(f)
	if err != nil {
		return nil, err
	}
	if r.impossible {
		return nil, nil
	}
	predG, argsG := r.predicates("g")
	predH, argsH := r.predicates("h")
	predH2, argsH2 := r.predicates("h2")
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
    FROM history g
    WHERE g.deleted = 0`)
	b.WriteString(predG)
	args = append(args, argsG...)
	b.WriteString(`
    GROUP BY command_hash`)
	if f.Sort == SortFailed {
		b.WriteString(`
    HAVING COUNT(*) FILTER (WHERE exit_status IS NOT NULL AND exit_status != 0) > 0`)
	}
	b.WriteString(`
) agg ON agg.command_hash = h.command_hash
WHERE h.deleted = 0`)
	b.WriteString(predH)
	args = append(args, argsH...)
	b.WriteString(`
AND NOT EXISTS (
    SELECT 1 FROM history h2
    WHERE h2.deleted = 0`)
	b.WriteString(predH2)
	args = append(args, argsH2...)
	b.WriteString(`
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
		return nil, fmt.Errorf("command summaries advanced: %w", err)
	}
	defer rows.Close()
	out, err := scanCommandSummaries(rows)
	if err != nil {
		return nil, err
	}
	if f.CommandRE != nil {
		matched := make([]CommandSummary, 0, len(out))
		for _, cs := range out {
			if f.CommandRE.MatchString(cs.Command) {
				matched = append(matched, cs)
			}
		}
		out = matched
		if f.Limit > 0 && len(out) > f.Limit {
			out = out[:f.Limit]
		}
	}
	return out, nil
}

func (s *Store) ListByCommandMatching(command string, f AdvancedFilter) ([]Entry, error) {
	if command == "" {
		return nil, nil
	}
	if f.CommandRE != nil && !f.CommandRE.MatchString(command) {
		return nil, nil
	}
	if !f.Active() {
		return s.ListByCommand(command)
	}
	r, err := s.resolveAdvanced(f)
	if err != nil {
		return nil, err
	}
	if r.impossible {
		return nil, nil
	}
	pred, predArgs := r.predicates("h")
	args := append([]any{command}, predArgs...)
	rows, err := s.db.Query(`SELECT `+historySelect("h")+`
FROM `+historyFrom("h")+` WHERE h.deleted = 0 AND h.command = ?`+pred+`
ORDER BY h.start_ts DESC, h.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntries(rows)
}

func (s *Store) resolveAdvanced(f AdvancedFilter) (resolvedAdvanced, error) {
	r := resolvedAdvanced{
		commandNeedle: f.CommandNeedle,
		exitFailed:    f.ExitFailed,
		exit:          f.Exit,
		since:         f.Since,
		until:         f.Until,
	}
	var err error
	if f.CwdRE != nil {
		r.cwd, err = s.resolveIntern("intern_cwd", f.CwdRE)
		if err != nil || r.cwd.impossible() {
			r.impossible = true
			return r, err
		}
	}
	if f.HostRE != nil {
		r.host, err = s.resolveIntern("intern_hostname", f.HostRE)
		if err != nil || r.host.impossible() {
			r.impossible = true
			return r, err
		}
	}
	if f.ShellRE != nil {
		r.shell, err = s.resolveIntern("intern_shell", f.ShellRE)
		if err != nil || r.shell.impossible() {
			r.impossible = true
			return r, err
		}
	}
	if f.SessionRE != nil {
		r.session, err = s.resolveIntern("intern_session", f.SessionRE)
		if err != nil || r.session.impossible() {
			r.impossible = true
			return r, err
		}
	}
	if f.DeviceRE != nil {
		r.device, err = s.resolveStrings(`SELECT DISTINCT device_id FROM history`, f.DeviceRE)
		if err != nil || r.device.impossible() {
			r.impossible = true
			return r, err
		}
	}
	if f.ExitRE != nil {
		r.exits, err = s.resolveExits(f.ExitRE)
		if err != nil || r.exits.impossible() {
			r.impossible = true
			return r, err
		}
	}
	return r, nil
}

func (m columnMatch) impossible() bool {
	return m.set && len(m.ids) == 0 && !m.allowNil
}

func (m stringMatch) impossible() bool {
	return m.set && len(m.vals) == 0 && !m.allowNil
}

func (m intMatch) impossible() bool {
	return m.set && len(m.vals) == 0 && !m.allowNil
}

func (s *Store) resolveIntern(table string, re *regexp.Regexp) (columnMatch, error) {
	switch table {
	case "intern_cwd", "intern_hostname", "intern_shell", "intern_session":
	default:
		return columnMatch{}, fmt.Errorf("unknown intern table %q", table)
	}
	rows, err := s.db.Query(`SELECT id, value FROM ` + table)
	if err != nil {
		return columnMatch{}, err
	}
	defer rows.Close()
	m := columnMatch{set: true, allowNil: re.MatchString("")}
	var total int
	for rows.Next() {
		var id int64
		var value string
		if err := rows.Scan(&id, &value); err != nil {
			return columnMatch{}, err
		}
		total++
		if re.MatchString(value) {
			m.ids = append(m.ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		return m, err
	}
	if total > 0 && len(m.ids) == total && m.allowNil {
		m.set = false
		m.ids = nil
		return m, nil
	}
	if total > 0 && len(m.ids) == total && !m.allowNil {
		m.notNil = true
		m.ids = nil
	}
	return m, nil
}

func (s *Store) resolveStrings(query string, re *regexp.Regexp) (stringMatch, error) {
	rows, err := s.db.Query(query)
	if err != nil {
		return stringMatch{}, err
	}
	defer rows.Close()
	m := stringMatch{set: true, allowNil: re.MatchString("")}
	var total int
	for rows.Next() {
		var value *string
		if err := rows.Scan(&value); err != nil {
			return stringMatch{}, err
		}
		total++
		v := ""
		if value != nil {
			v = *value
		}
		if re.MatchString(v) {
			m.vals = append(m.vals, v)
		}
	}
	if err := rows.Err(); err != nil {
		return m, err
	}
	if total > 0 && len(m.vals) == total && m.allowNil {
		m.set = false
		m.vals = nil
		return m, nil
	}
	if total > 0 && len(m.vals) == total && !m.allowNil {
		m.notNil = true
		m.vals = nil
	}
	return m, nil
}

func (s *Store) resolveExits(re *regexp.Regexp) (intMatch, error) {
	rows, err := s.db.Query(`SELECT DISTINCT exit_status FROM history WHERE deleted = 0`)
	if err != nil {
		return intMatch{}, err
	}
	defer rows.Close()
	m := intMatch{set: true, allowNil: re.MatchString("")}
	var total int
	for rows.Next() {
		var raw *int64
		if err := rows.Scan(&raw); err != nil {
			return intMatch{}, err
		}
		total++
		if raw == nil {
			if m.allowNil {
				continue
			}
			continue
		}
		if re.MatchString(strconv.FormatInt(*raw, 10)) {
			m.vals = append(m.vals, int(*raw))
		}
	}
	if err := rows.Err(); err != nil {
		return m, err
	}
	if total > 0 && len(m.vals) == total && m.allowNil {
		m.set = false
		m.vals = nil
		return m, nil
	}
	return m, nil
}

func (r resolvedAdvanced) predicates(alias string) (string, []any) {
	p := alias
	if p == "" {
		p = "h"
	}
	col := func(name string) string {
		return p + "." + name
	}
	var b strings.Builder
	var args []any
	appendColumn := func(c columnMatch, name string) {
		if !c.set {
			return
		}
		if c.notNil {
			b.WriteString(" AND " + col(name) + " IS NOT NULL")
			return
		}
		switch {
		case len(c.ids) == 0 && c.allowNil:
			b.WriteString(" AND " + col(name) + " IS NULL")
		case c.allowNil:
			b.WriteString(" AND (" + col(name) + " IN (" + placeholders(len(c.ids)) + ") OR " + col(name) + " IS NULL)")
			for _, id := range c.ids {
				args = append(args, id)
			}
		default:
			b.WriteString(" AND " + col(name) + " IN (" + placeholders(len(c.ids)) + ")")
			for _, id := range c.ids {
				args = append(args, id)
			}
		}
	}
	appendStrings := func(c stringMatch, name string) {
		if !c.set {
			return
		}
		if c.notNil {
			b.WriteString(" AND " + col(name) + " IS NOT NULL AND " + col(name) + " != ''")
			return
		}
		switch {
		case len(c.vals) == 0 && c.allowNil:
			b.WriteString(" AND (" + col(name) + " IS NULL OR " + col(name) + " = '')")
		case c.allowNil:
			b.WriteString(" AND (" + col(name) + " IN (" + placeholders(len(c.vals)) + ") OR " + col(name) + " IS NULL OR " + col(name) + " = '')")
			for _, v := range c.vals {
				args = append(args, v)
			}
		default:
			b.WriteString(" AND " + col(name) + " IN (" + placeholders(len(c.vals)) + ")")
			for _, v := range c.vals {
				args = append(args, v)
			}
		}
	}
	appendInts := func(c intMatch, name string) {
		if !c.set {
			return
		}
		switch {
		case len(c.vals) == 0 && c.allowNil:
			b.WriteString(" AND " + col(name) + " IS NULL")
		case c.allowNil:
			b.WriteString(" AND (" + col(name) + " IN (" + placeholders(len(c.vals)) + ") OR " + col(name) + " IS NULL)")
			for _, v := range c.vals {
				args = append(args, v)
			}
		default:
			b.WriteString(" AND " + col(name) + " IN (" + placeholders(len(c.vals)) + ")")
			for _, v := range c.vals {
				args = append(args, v)
			}
		}
	}
	appendColumn(r.cwd, "cwd_id")
	appendColumn(r.host, "hostname_id")
	appendColumn(r.shell, "shell_id")
	appendColumn(r.session, "session_id")
	appendStrings(r.device, "device_id")
	appendInts(r.exits, "exit_status")
	if r.exitFailed {
		b.WriteString(" AND " + col("exit_status") + " IS NOT NULL AND " + col("exit_status") + " != 0")
	}
	if r.exit != nil {
		b.WriteString(" AND " + col("exit_status") + " = ?")
		args = append(args, *r.exit)
	}
	if r.since != nil {
		b.WriteString(" AND " + col("start_ts") + " >= ?")
		args = append(args, r.since.UnixMilli())
	}
	if r.until != nil {
		b.WriteString(" AND " + col("start_ts") + " <= ?")
		args = append(args, r.until.UnixMilli())
	}
	if r.commandNeedle != "" {
		b.WriteString(" AND instr(lower(" + col("command") + "), lower(?)) > 0")
		args = append(args, r.commandNeedle)
	}
	return b.String(), args
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}
