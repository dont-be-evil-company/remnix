package db

import (
	"database/sql"
	"fmt"
)

type PageInfo struct {
	PageSize  int64
	PageCount int64
	Freelist  int64
	Bytes     int64
}

type BTreeStat struct {
	Name    string
	Pages   int64
	Payload int64
	Unused  int64
	Size    int64
}

func PageInfoOf(sqlDB *sql.DB) (PageInfo, error) {
	var info PageInfo
	if err := sqlDB.QueryRow(`SELECT page_size FROM pragma_page_size`).Scan(&info.PageSize); err != nil {
		return PageInfo{}, err
	}
	if err := sqlDB.QueryRow(`SELECT page_count FROM pragma_page_count`).Scan(&info.PageCount); err != nil {
		return PageInfo{}, err
	}
	if err := sqlDB.QueryRow(`SELECT freelist_count FROM pragma_freelist_count`).Scan(&info.Freelist); err != nil {
		return PageInfo{}, err
	}
	info.Bytes = info.PageSize * info.PageCount
	return info, nil
}

func BTreeStats(sqlDB *sql.DB) ([]BTreeStat, error) {
	rows, err := sqlDB.Query(`
SELECT name, COUNT(*), SUM(payload), SUM(unused), SUM(pgsize)
FROM dbstat
GROUP BY name
ORDER BY SUM(pgsize) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BTreeStat
	for rows.Next() {
		var s BTreeStat
		if err := rows.Scan(&s.Name, &s.Pages, &s.Payload, &s.Unused, &s.Size); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func Compact(sqlDB *sql.DB) error {
	if err := CheckpointWAL(sqlDB); err != nil {
		return fmt.Errorf("wal checkpoint: %w", err)
	}
	if _, err := sqlDB.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	return CheckpointWAL(sqlDB)
}

func HistoryEventPayloads(sqlDB *sql.DB) (int64, error) {
	var n int64
	err := sqlDB.QueryRow(`
SELECT COUNT(*) FROM sync_events
WHERE payload IS NOT NULL AND length(payload) > 0
  AND event_type IN ('history-created', 'history-tombstoned')`).Scan(&n)
	return n, err
}

func FormatBytes(n int64) string {
	const (
		kiB = 1024
		miB = 1024 * 1024
	)
	switch {
	case n >= miB:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(miB))
	case n >= kiB:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(kiB))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
