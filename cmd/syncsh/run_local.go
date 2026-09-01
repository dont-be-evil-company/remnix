package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"
	"github.com/mistweaverco/syncsh/internal/agent"
	"github.com/mistweaverco/syncsh/internal/app"
	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/importers"
	"github.com/mistweaverco/syncsh/internal/search"
	"github.com/mistweaverco/syncsh/internal/shell"
	"github.com/mistweaverco/syncsh/internal/stats"
	"github.com/mistweaverco/syncsh/internal/tui"
	"github.com/spf13/cobra"
)

type searchOptions struct {
	Query       string
	Cwd         string
	Host        string
	Shell       string
	Session     string
	Exit        int
	ExitSet     bool
	Limit       int
	Exact       bool
	Interactive bool
	Explain     bool
}

func openApp() (*app.App, error) {
	a, err := app.Open()
	if err != nil {
		return nil, err
	}
	if err := a.EnsureLocalDevice(); err != nil {
		_ = a.Close()
		return nil, err
	}
	return a, nil
}

func runTUI(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	store := history.NewStore(a.DB)
	entries, err := store.List(history.Filter{Limit: 5000, Unique: true})
	if err != nil {
		return err
	}
	return tui.RunOpts(entries, tui.Options{
		Delete:   func(e history.Entry) error { return a.TombstoneCommand(e.Command) },
		DeviceID: a.Config.DeviceID,
	}, cmd.OutOrStdout())
}

func runSearch(cmd *cobra.Command, opts searchOptions) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	f := history.Filter{
		Host:    opts.Host,
		Shell:   opts.Shell,
		Session: opts.Session,
		Limit:   5000,
		Unique:  opts.Interactive,
	}
	if !opts.Interactive {
		f.Cwd = opts.Cwd
	}
	if opts.Exact {
		f.Query = opts.Query
	}
	if opts.ExitSet {
		v := opts.Exit
		f.Exit = &v
	}
	store := history.NewStore(a.DB)
	entries, err := store.List(f)
	if err != nil {
		return err
	}
	if opts.Interactive {
		cwd := opts.Cwd
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		return tui.RunOpts(entries, tui.Options{
			Query:     opts.Query,
			Cwd:       cwd,
			DeviceID:  a.Config.DeviceID,
			SessionID: opts.Session,
			Widget:    true,
			Delete:    func(e history.Entry) error { return a.TombstoneCommand(e.Command) },
		}, cmd.OutOrStdout())
	}
	results := search.RankWith(opts.Query, entries, opts.Exact, search.Context{
		Cwd:       opts.Cwd,
		DeviceID:  a.Config.DeviceID,
		SessionID: opts.Session,
	})
	n := opts.Limit
	if n <= 0 || n > len(results) {
		n = len(results)
	}
	for _, r := range results[:n] {
		if opts.Explain {
			fmt.Fprintln(cmd.OutOrStdout(), search.Explain(r))
			continue
		}
		fmt.Fprintln(cmd.OutOrStdout(), r.Entry.Command)
	}
	return nil
}

func runStats(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	st, err := history.NewStore(a.DB).Stats()
	if err != nil {
		return err
	}
	stats.Write(cmd.OutOrStdout(), st)
	return nil
}

func runInspect(_ *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	store := history.NewStore(a.DB)
	load := func(sort history.SummarySort, query string) ([]history.CommandSummary, history.Stats, error) {
		st, err := store.Stats()
		if err != nil {
			return nil, st, err
		}
		sums, err := store.CommandSummaries(history.SummaryFilter{
			Query: query,
			Sort:  sort,
			Limit: 5000,
		})
		return sums, st, err
	}
	summaries, st, err := load(history.SortRecent, "")
	if err != nil {
		return err
	}
	return tui.RunInspect(tui.InspectOptions{
		Stats:     st,
		Summaries: summaries,
		Load:      load,
		ListRuns:  store.ListByCommand,
		DeleteCommand: func(command string) error {
			return a.TombstoneCommand(command)
		},
		DeleteEntry: func(e history.Entry) error {
			return a.TombstoneEntries([]history.Entry{e})
		},
	})
}

func runSuggest(cmd *cobra.Command, prefix, cwd string, list bool) error {
	if prefix == "" {
		return nil
	}
	if list {
		items, err := agent.DialRPCList("suggest-list", prefix, cwd)
		if err != nil {
			a, err := openApp()
			if err != nil {
				return err
			}
			defer a.Close()
			items, err = agent.NewService(a).SuggestList(prefix, cwd)
			if err != nil {
				return err
			}
		}
		for _, s := range items {
			fmt.Fprintln(cmd.OutOrStdout(), s)
		}
		return nil
	}
	if s, err := agent.DialRPC("suggest", prefix, cwd); err == nil {
		if s != "" {
			fmt.Fprintln(cmd.OutOrStdout(), s)
		}
		return nil
	}
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	s, err := agent.NewService(a).Suggest(prefix, cwd)
	if err != nil {
		return err
	}
	if s != "" {
		fmt.Fprintln(cmd.OutOrStdout(), s)
	}
	return nil
}

func runInit(cmd *cobra.Command, args []string) error {
	bin, err := os.Executable()
	if err != nil {
		bin = "syncsh"
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	out, err := shell.Integration(args[0], bin, shell.Options{
		SuggestEnabled: cfg.Suggest.IsEnabled(),
		SuggestAccept:  cfg.Suggest.AcceptKeys(),
		SuggestMenu:    cfg.Suggest.MenuEnabled(),
	})
	if err != nil {
		return err
	}
	fmt.Fprint(cmd.OutOrStdout(), out)
	return nil
}

func runHistoryStart(cmd *cobra.Command, _ []string) error {
	command, _ := cmd.Flags().GetString("command")
	cwd, _ := cmd.Flags().GetString("cwd")
	session, _ := cmd.Flags().GetString("session")
	sh, _ := cmd.Flags().GetString("shell")
	if id, err := agent.DialRPC("start", command, cwd, session, sh); err == nil {
		fmt.Fprintln(cmd.OutOrStdout(), id)
		return nil
	}
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	id, err := agent.NewService(a).Start(command, cwd, session, sh)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), id)
	return nil
}

func runHistoryEnd(cmd *cobra.Command, _ []string) error {
	id, _ := cmd.Flags().GetString("id")
	exit, _ := cmd.Flags().GetInt("exit")
	if _, err := agent.DialRPC("end", id, strconv.Itoa(exit)); err == nil {
		return nil
	}
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	return agent.NewService(a).End(id, exit)
}

func runImportHistfile(cmd *cobra.Command, args []string) error {
	path := ""
	if len(args) > 0 {
		path = args[0]
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(home, ".zsh_history")
	}
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	recs, err := importers.ReadHistfile(path, "auto")
	if err != nil {
		return err
	}
	kind := importers.DetectKind(path, "")
	entries := importers.ToEntries(recs, a.Config.DeviceID, kind)
	return importEntries(cmd, a, entries)
}

func runImportAtuin(cmd *cobra.Command, args []string) error {
	path := ""
	if len(args) > 0 {
		path = args[0]
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path = filepath.Join(home, ".local", "share", "atuin", "history.db")
	}
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	entries, err := importers.ImportAtuin(path, a.Config.DeviceID)
	if err != nil {
		return err
	}
	return importEntries(cmd, a, entries)
}

func importEntries(cmd *cobra.Command, a *app.App, entries []history.Entry) error {
	store := history.NewStore(a.DB)
	var inserted int
	for _, e := range entries {
		if history.ShouldSkip(e.Command) {
			continue
		}
		if e.ID == "" {
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			e.ID = id.String()
		}
		ok, err := store.Insert(e)
		if err != nil {
			return err
		}
		if ok {
			inserted++
			if err := a.EnqueueHistoryCreated(e); err != nil {
				return err
			}
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "imported %d new of %d records\n", inserted, len(entries))
	return nil
}
