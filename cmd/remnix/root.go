package main

import (
	"log/slog"
	"os"

	"github.com/dont-be-evil-company/remnix/internal/redact"
	"github.com/dont-be-evil-company/remnix/internal/version"
	"github.com/spf13/cobra"
)

var (
	debugFlag   bool
	versionFlag bool
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "remnix",
		Short:        "Encrypted, server-free shell history manager",
		Long:         "remnix stores shell history in a local SQLite database and synchronizes encrypted events through a filesystem-backed remote.",
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			level := slog.LevelInfo
			if debugFlag {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level:       level,
				ReplaceAttr: redact.ReplaceAttr,
			})))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if versionFlag {
				cmd.Println(version.Version)
				return nil
			}
			return runTUI(cmd, args)
		},
	}
	cmd.PersistentFlags().BoolVarP(&debugFlag, "debug", "d", false, "enable debug logging")
	cmd.Flags().BoolVar(&versionFlag, "version", false, "print version and exit")

	cmd.AddCommand(newSearchCmd())
	cmd.AddCommand(newSuggestCmd())
	cmd.AddCommand(newAgentCmd())
	cmd.AddCommand(newPtyProxyCmd())
	cmd.AddCommand(newStatsCmd())
	cmd.AddCommand(newInspectCmd())
	cmd.AddCommand(newInitCmd())
	cmd.AddCommand(newImportCmd())
	cmd.AddCommand(newHistoryCmd())
	cmd.AddCommand(newSyncCmd())
	cmd.AddCommand(newSetupCmd())
	cmd.AddCommand(newDeviceCmd())
	cmd.AddCommand(newKeyCmd())
	cmd.AddCommand(newGCCmd())
	cmd.AddCommand(newDatabaseCmd())
	cmd.AddCommand(newDoctorCmd())
	cmd.AddCommand(newUnlockCmd())
	cmd.AddCommand(newDaemonCmd())
	cmd.AddCommand(newCompletionCmd())
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newUpdateCmd())
	cmd.AddCommand(newChangelogCmd())
	cmd.AddCommand(newConfigCmd())
	cmd.AddCommand(newRemoteCmd())
	return cmd
}

func newVersionCmd() *cobra.Command {
	var verbose bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the remnix version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if verbose {
				cmd.Print(version.Verbose())
				return nil
			}
			cmd.Println(version.Version)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "include embedded rclone engine version")
	return cmd
}

func Execute() error {
	return newRootCmd().Execute()
}
