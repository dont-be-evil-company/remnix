package main

import (
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
	for _, s := range st {
		if !s.Applied {
			fmt.Fprintf(cmd.OutOrStdout(), "pending migration: %s\n", s.ID)
			ok = false
		}
		if s.Mismatch {
			fmt.Fprintf(cmd.OutOrStdout(), "checksum mismatch: %s\n", s.ID)
			ok = false
		}
	}
	if !ok {
		return fmt.Errorf("database doctor found problems")
	}
	fmt.Fprintln(cmd.OutOrStdout(), "database: ok")
	return nil
}
