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
		resultFile  string
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
				ResultFile:  resultFile,
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
	cmd.Flags().StringVar(&resultFile, "result-file", "", "write the selected command to this file (nushell); otherwise widget mode prints it on stderr")
	return cmd
}

func newStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show local history statistics",
		RunE:  runStats,
	}
}

func newInspectCmd() *cobra.Command {
	var advanced bool
	cmd := &cobra.Command{
		Use:     "inspect",
		Aliases: []string{"explore"},
		Short:   "Explore local history statistics",
		Long: `Explore unique commands and their runs.

ctrl+f (or --advanced) opens advanced search: a left sidebar of regex
criteria (ctrl+s toggles criteria vs results), multi-select delete of
matching runs, and per-command usage charts in the run view (g cycles
day/month/year).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInspect(advanced)
		},
	}
	cmd.Flags().BoolVar(&advanced, "advanced", false, "open advanced search with a criteria sidebar")
	return cmd
}

func newSuggestCmd() *cobra.Command {
	var prefix, cwd, resultFile, itemsFile string
	var list, interactive bool
	cmd := &cobra.Command{
		Use:   "suggest",
		Short: "Print the best history prefix match for inline shell suggestions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSuggest(cmd, prefix, cwd, list, interactive, resultFile, itemsFile)
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "typed command prefix")
	cmd.Flags().StringVar(&cwd, "cwd", "", "working directory used for ranking")
	cmd.Flags().BoolVar(&list, "list", false, "print ranked prefix matches, one per line")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "open the suggestion overlay TUI")
	cmd.Flags().StringVar(&resultFile, "result-file", "", "write the selected command to this file (nushell); otherwise widget mode prints it on stderr")
	cmd.Flags().StringVar(&itemsFile, "items-file", "", "newline-separated completion lines for the overlay (instead of history)")
	cmd.AddCommand(newSuggestCacheCmd())
	return cmd
}

func newSuggestCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the persistent CLI help cache for suggestions",
	}
	cmd.AddCommand(newSuggestCachePurgeCmd(), newSuggestCacheWarmupCmd())
	return cmd
}

func newSuggestCachePurgeCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "purge [tool...]",
		Short: "Drop cached --help data for the given CLI tools",
		Long:  "Remove parsed help pages for tools such as aws or gcloud. Overlay will probe again on the next use unless you run warmup.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSuggestCachePurge(cmd, args, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "wipe the entire suggest cache")
	return cmd
}

func newSuggestCacheWarmupCmd() *cobra.Command {
	var jobs, depth int
	var status, foreground, worker bool
	cmd := &cobra.Command{
		Use:   "warmup [tool...]",
		Short: "Pre-populate the suggest cache from CLI --help output",
		Long: `Walk CLI --help trees into suggest-cache.db.

Warmup starts in the background by default. Attach to live progress with:

  remnix suggest cache warmup --status

Use --foreground to run in this terminal. A second warmup while one is
running enqueues those tools onto the same worker.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSuggestCacheWarmup(cmd, args, jobs, depth, status, foreground, worker)
		},
	}
	cmd.Flags().IntVar(&jobs, "jobs", 4, "concurrent help probes")
	cmd.Flags().IntVar(&depth, "depth", 0, "max command depth to walk (0 = unlimited)")
	cmd.Flags().BoolVar(&status, "status", false, "attach to live warmup progress")
	cmd.Flags().BoolVar(&foreground, "foreground", false, "run in this terminal instead of the background")
	cmd.Flags().BoolVar(&worker, "worker", false, "run as the detached warmup worker")
	_ = cmd.Flags().MarkHidden("worker")
	return cmd
}

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init {zsh|bash|fish|nu}",
		Short: "Print shell integration for the given shell",
		Args:  cobra.ExactArgs(1),
		RunE:  runInit,
	}
}

func newPtyProxyCmd() *cobra.Command {
	var shellPath string
	cmd := &cobra.Command{
		Use:    "pty-proxy",
		Short:  "Wrap a shell in a terminal proxy for overlay TUIs",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPtyProxy(shellPath)
		},
	}
	cmd.Flags().StringVar(&shellPath, "shell", "", "absolute path of the shell to spawn")
	return cmd
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
