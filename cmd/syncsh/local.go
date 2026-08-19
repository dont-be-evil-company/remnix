package main

import (
	"github.com/spf13/cobra"
)

func newSearchCmd() *cobra.Command {
	var (
		cwd         string
		host        string
		shell       string
		session     string
		queryFlag   string
		exit        int
		exitSet     bool
		limit       int
		exact       bool
		interactive bool
		explain     bool
	)
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search local shell history",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := queryFlag
			if query == "" && len(args) > 0 {
				query = args[0]
			}
			if cmd.Flags().Changed("exit") {
				exitSet = true
			}
			return runSearch(cmd, searchOptions{
				Query:       query,
				Cwd:         cwd,
				Host:        host,
				Shell:       shell,
				Session:     session,
				Exit:        exit,
				ExitSet:     exitSet,
				Limit:       limit,
				Exact:       exact,
				Interactive: interactive,
				Explain:     explain,
			})
		},
	}
	cmd.Flags().StringVar(&queryFlag, "query", "", "initial search query")
	cmd.Flags().StringVar(&cwd, "cwd", "", "filter by working directory")
	cmd.Flags().StringVar(&host, "host", "", "filter by hostname")
	cmd.Flags().StringVar(&shell, "shell", "", "filter by shell")
	cmd.Flags().StringVar(&session, "session", "", "filter by session id")
	cmd.Flags().IntVar(&exit, "exit", 0, "filter by exit code")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum results")
	cmd.Flags().BoolVar(&exact, "exact", false, "substring match instead of fuzzy ranking")
	cmd.Flags().BoolVar(&explain, "explain", false, "print ranking score breakdown")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "open fuzzy search TUI (Ctrl+R widget)")
	return cmd
}

func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show local history statistics",
		RunE:  runStats,
	}
}

func newSuggestCmd() *cobra.Command {
	var prefix, cwd string
	cmd := &cobra.Command{
		Use:   "suggest",
		Short: "Print the best history prefix match for inline shell suggestions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSuggest(cmd, prefix, cwd)
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "typed command prefix")
	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory used for ranking")
	return cmd
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init {zsh|bash|fish}",
		Short: "Print shell integration for the given shell",
		Args:  cobra.ExactArgs(1),
		RunE:  runInit,
	}
}

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import history from existing sources",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "histfile [path]",
		Short: "Import a shell history file (zsh, bash, or fish)",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runImportHistfile,
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "atuin [path]",
		Short: "Import an Atuin SQLite history database",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runImportAtuin,
	})
	return cmd
}

func newHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Record command lifecycle events",
	}
	start := &cobra.Command{
		Use:   "start",
		Short: "Record the start of a command; prints the history id",
		RunE:  runHistoryStart,
	}
	start.Flags().String("command", "", "command text")
	start.Flags().String("cwd", "", "working directory")
	start.Flags().String("session", "", "session identifier")
	start.Flags().String("shell", "", "shell name")
	_ = start.MarkFlagRequired("command")

	end := &cobra.Command{
		Use:   "end",
		Short: "Record command completion",
		RunE:  runHistoryEnd,
	}
	end.Flags().String("id", "", "history id from history start")
	end.Flags().Int("exit", 0, "exit status")
	_ = end.MarkFlagRequired("id")

	cmd.AddCommand(start, end)
	return cmd
}

func newCompletionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "completion {bash|zsh|fish}",
		Short: "Generate shell completion scripts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				return cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				return cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			default:
				return cmd.Help()
			}
		},
	}
}
