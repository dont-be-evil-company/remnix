package main

import (
	"database/sql"
	"fmt"

	"github.com/mistweaverco/syncsh/internal/app"
	"github.com/mistweaverco/syncsh/internal/db"
	"github.com/spf13/cobra"
)

func runDatabaseMigrate(cmd *cobra.Command, _ []string) error {
	a, err := app.OpenOpts(app.OpenOptions{SkipMigrate: true})
	if err != nil {
		return err
	}
	defer a.Close()
	if err := db.Migrate(a.DB.SQL); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "migrations applied")
	return nil
}

func runDatabaseStatus(cmd *cobra.Command, _ []string) error {
	a, err := app.OpenOpts(app.OpenOptions{SkipMigrate: true})
	if err != nil {
		return err
	}
	defer a.Close()
	st, err := db.MigrationStatus(a.DB.SQL)
	if err != nil {
		return err
	}
	for _, s := range st {
		state := "pending"
		if s.Applied {
			state = "applied"
		}
		if s.Mismatch {
			state = "CHECKSUM MISMATCH"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", s.ID, state, s.Checksum[:12])
	}
	return nil
}

func runDatabaseDoctor(cmd *cobra.Command, _ []string) error {
	a, err := app.OpenOpts(app.OpenOptions{SkipMigrate: true})
	if err != nil {
		return err
	}
	defer a.Close()
	ok := true
	result, err := db.IntegrityCheck(a.DB.SQL)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "integrity_check: %s\n", result)
	if result != "ok" {
		ok = false
	}
	st, err := db.MigrationStatus(a.DB.SQL)
	if err != nil {
		return err
	}
	thinApplied := false
	for _, s := range st {
		if !s.Applied {
			fmt.Fprintf(cmd.OutOrStdout(), "pending migration: %s\n", s.ID)
			ok = false
		}
		if s.Mismatch {
			fmt.Fprintf(cmd.OutOrStdout(), "checksum mismatch: %s\n", s.ID)
			ok = false
		}
		if s.ID == "0003_thin_events" && s.Applied {
			thinApplied = true
		}
	}
	if thinApplied {
		n, err := db.HistoryEventPayloads(a.DB.SQL)
		if err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "sync_events payloads: %v\n", err)
			ok = false
		} else if n > 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "sync_events still stores %d history payloads\n", n)
			ok = false
		}
	}
	if err := printDatabaseStats(cmd, a.DB.SQL); err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "dbstat: %v\n", err)
	}
	if !ok {
		return fmt.Errorf("database doctor found problems")
	}
	fmt.Fprintln(cmd.OutOrStdout(), "database: ok")
	return nil
}

func runDatabaseCompact(cmd *cobra.Command, _ []string) error {
	a, err := app.Open()
	if err != nil {
		return err
	}
	defer a.Close()
	if err := db.Compact(a.DB.SQL); err != nil {
		return err
	}
	if err := printDatabaseStats(cmd, a.DB.SQL); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "compacted")
	return nil
}

func printDatabaseStats(cmd *cobra.Command, sqlDB *sql.DB) error {
	info, err := db.PageInfoOf(sqlDB)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "pages: %d x %d = %s (freelist %d)\n",
		info.PageCount, info.PageSize, db.FormatBytes(info.Bytes), info.Freelist)
	stats, err := db.BTreeStats(sqlDB)
	if err != nil {
		return err
	}
	limit := 12
	if len(stats) < limit {
		limit = len(stats)
	}
	for _, s := range stats[:limit] {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\t%s\n", s.Name, db.FormatBytes(s.Size))
	}
	return nil
}
