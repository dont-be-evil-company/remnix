package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/dont-be-evil-company/remnix/internal/app"
	"github.com/dont-be-evil-company/remnix/internal/client"
	"github.com/dont-be-evil-company/remnix/internal/config"
	"github.com/dont-be-evil-company/remnix/internal/helpparse"
	"github.com/dont-be-evil-company/remnix/internal/history"
	"github.com/dont-be-evil-company/remnix/internal/importers"
	"github.com/dont-be-evil-company/remnix/internal/protocol"
	"github.com/dont-be-evil-company/remnix/internal/ptyproxy"
	"github.com/dont-be-evil-company/remnix/internal/search"
	"github.com/dont-be-evil-company/remnix/internal/shell"
	"github.com/dont-be-evil-company/remnix/internal/stats"
	"github.com/dont-be-evil-company/remnix/internal/suggestcache"
	"github.com/dont-be-evil-company/remnix/internal/tui"
	"github.com/google/uuid"
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
	ResultFile  string
}

func uiTheme(cfg *config.Config) tui.Theme {
	if cfg == nil {
		if loaded, err := config.Load(); err == nil {
			return tui.NewTheme(loaded.UI)
		}
		return tui.DefaultTheme()
	}
	return tui.NewTheme(cfg.UI)
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
		Delete: func(e history.Entry) error {
			if err := client.HistoryDelete(e.Command); err == nil {
				return nil
			}
			return a.TombstoneCommand(e.Command)
		},
		DeviceID: a.Config.DeviceID,
		Theme:    uiTheme(a.Config),
	}, cmd.OutOrStdout())
}

func runSearch(cmd *cobra.Command, opts searchOptions) error {
	if hits, err := client.HistorySearch(clientSearchReq(opts)); err == nil {
		entries := client.HitsToEntries(hits)
		if opts.Interactive {
			return runSearchTUI(cmd, opts, entries, nil)
		}
		return writeSearchResults(cmd, opts, entries, "")
	}
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
		return runSearchTUI(cmd, opts, entries, a)
	}
	return writeSearchResults(cmd, opts, entries, a.Config.DeviceID)
}

func clientSearchReq(opts searchOptions) protocol.HistorySearchReq {
	limit := opts.Limit
	if limit <= 0 || opts.Interactive {
		limit = 5000
	}
	return protocol.HistorySearchReq{
		Query:     opts.Query,
		Cwd:       opts.Cwd,
		SessionID: opts.Session,
		Host:      opts.Host,
		Shell:     opts.Shell,
		Limit:     limit,
		Exact:     opts.Exact,
		Unique:    opts.Interactive,
	}
}

func runSearchTUI(cmd *cobra.Command, opts searchOptions, entries []history.Entry, a *app.App) error {
	cwd := opts.Cwd
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	deviceID := ""
	sessionID := opts.Session
	overlay := 100
	var cfg *config.Config
	if a != nil {
		deviceID = a.Config.DeviceID
		overlay = a.Config.PtyProxy.HeightPercent()
		cfg = a.Config
	}
	return tui.RunOpts(entries, tui.Options{
		Query:          opts.Query,
		Cwd:            cwd,
		DeviceID:       deviceID,
		SessionID:      sessionID,
		Widget:         true,
		OverlayPercent: overlay,
		Theme:          uiTheme(cfg),
		ResultFile:     opts.ResultFile,
		Delete: func(e history.Entry) error {
			if err := client.HistoryDelete(e.Command); err == nil {
				return nil
			}
			if a != nil {
				return a.TombstoneCommand(e.Command)
			}
			return client.HistoryDelete(e.Command)
		},
	}, cmd.OutOrStdout())
}

func writeSearchResults(cmd *cobra.Command, opts searchOptions, entries []history.Entry, deviceID string) error {
	results := search.RankWith(opts.Query, entries, opts.Exact, search.Context{
		Cwd:       opts.Cwd,
		DeviceID:  deviceID,
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

func runInspect(advanced bool) error {
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
		Stats:            st,
		Summaries:        summaries,
		Load:             load,
		StartAdvanced:    advanced,
		ListRuns:         store.ListByCommand,
		ListMatchingRuns: store.ListByCommandMatching,
		AdvancedLoad: func(sort history.SummarySort, f history.AdvancedFilter) ([]history.CommandSummary, history.Stats, error) {
			st, err := store.Stats()
			if err != nil {
				return nil, st, err
			}
			f.Sort = sort
			if f.Limit == 0 {
				f.Limit = 5000
			}
			sums, err := store.CommandSummariesAdvanced(f)
			return sums, st, err
		},
		Theme: uiTheme(a.Config),
		DeleteCommand: func(command string) error {
			if err := client.HistoryDelete(command); err == nil {
				return nil
			}
			return a.TombstoneCommand(command)
		},
		DeleteEntry: func(e history.Entry) error {
			if err := client.TombstoneEntries([]history.Entry{e}); err == nil {
				return nil
			}
			return a.TombstoneEntries([]history.Entry{e})
		},
		DeleteEntries: func(entries []history.Entry) error {
			if err := client.TombstoneEntries(entries); err == nil {
				return nil
			}
			return a.TombstoneEntries(entries)
		},
	})
}

func runSuggest(cmd *cobra.Command, prefix, cwd string, list, interactive bool, resultFile, itemsFile string) error {
	if interactive {
		return runSuggestInteractive(cmd, prefix, cwd, resultFile, itemsFile)
	}
	if prefix == "" {
		return nil
	}
	if list {
		items, err := loadSuggestList(prefix, cwd)
		if err != nil {
			return err
		}
		for _, s := range items {
			fmt.Fprintln(cmd.OutOrStdout(), s)
		}
		return nil
	}
	if s, err := client.Suggest(prefix, cwd); err == nil {
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
	svc := history.NewService(history.NewStore(a.DB), history.NewCache(), a.DB.SQL, a.Config.DeviceID, nil, nil)
	_ = svc.Rebuild(context.Background())
	cands, err := svc.SuggestCandidates(prefix, cwd)
	if err != nil {
		return err
	}
	s := search.BestSuggestion(prefix, cands, search.Context{Cwd: cwd, DeviceID: a.Config.DeviceID})
	if err != nil {
		return err
	}
	if s != "" {
		fmt.Fprintln(cmd.OutOrStdout(), s)
	}
	return nil
}

func loadSuggestList(prefix, cwd string) ([]string, error) {
	items, err := client.SuggestList(prefix, cwd)
	if err == nil {
		return items, nil
	}
	a, err := openApp()
	if err != nil {
		return nil, err
	}
	defer a.Close()
	if prefix != "" {
		svc := history.NewService(history.NewStore(a.DB), history.NewCache(), a.DB.SQL, a.Config.DeviceID, nil, nil)
		_ = svc.Rebuild(context.Background())
		cands, err := svc.SuggestCandidates(prefix, cwd)
		if err != nil {
			return nil, err
		}
		return search.Suggestions(prefix, cands, search.Context{Cwd: cwd, DeviceID: a.Config.DeviceID}, a.Config.Suggest.MenuLimit()), nil
	}
	entries, err := history.NewStore(a.DB).List(history.Filter{Unique: true, Limit: 5000})
	if err != nil {
		return nil, err
	}
	return search.Suggestions(prefix, entries, search.Context{
		Cwd:      cwd,
		DeviceID: a.Config.DeviceID,
	}, a.Config.Suggest.MenuLimit()), nil
}

func runSuggestInteractive(cmd *cobra.Command, prefix, cwd, resultFile, itemsFile string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	itemIco := cfg.Suggest.IconHistory()
	var items, descrs []string
	if itemsFile != "" {
		items, descrs, err = loadSuggestItemsFile(itemsFile)
		if err != nil {
			return err
		}
		itemIco = cfg.Suggest.IconCompletion()
	} else {
		items, err = loadSuggestList(prefix, cwd)
		if err != nil {
			return err
		}
	}
	ch := make(chan tui.SuggestItems, 16)
	uiDone := make(chan struct{})
	go func() {
		defer close(ch)
		push := func(payload tui.SuggestItems) {
			select {
			case ch <- payload:
			case <-uiDone:
			}
		}
		push(tui.SuggestItems{Items: items, Descrs: descrs})
		if !helpparse.NeedsEnrich(items, descrs) {
			return
		}
		cache := helpparse.NewCache()
		if store, err := suggestcache.Open(config.SuggestCachePath()); err == nil {
			defer store.Close()
			cache.SetDurable(store)
		}
		d := helpparse.FillEmpty(context.Background(), prefix, items, descrs, cache, nil)
		push(tui.SuggestItems{Items: items, Descrs: d})
		if !helpparse.NeedsEnrich(items, d) {
			return
		}
		helpparse.FillNodes(context.Background(), prefix, items, d, cache, nil, 0, func(next []string) {
			push(tui.SuggestItems{Items: items, Descrs: next})
		})
	}()
	h := cfg.Suggest.MenuLimit() + 3
	err = tui.RunSuggestMenu(tui.SuggestMenuOptions{
		Prefix:        prefix,
		TypedIcon:     cfg.Suggest.IconTyped(),
		HistoryIcon:   cfg.Suggest.IconHistory(),
		ItemIcon:      itemIco,
		OverlayHeight: h,
		Widget:        true,
		ResultFile:    resultFile,
		ItemsCh:       ch,
		Theme:         uiTheme(cfg),
	}, cmd.OutOrStdout())
	close(uiDone)
	return err
}

func loadSuggestItemsFile(path string) (items, descrs []string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		cmd, descr, _ := strings.Cut(line, "\t")
		if cmd == "" {
			continue
		}
		items = append(items, cmd)
		descrs = append(descrs, descr)
		if len(items) >= 512 {
			break
		}
	}
	return items, descrs, sc.Err()
}

func runPtyProxy(shellPath string) error {
	err := ptyproxy.Run(shellPath)
	var ee ptyproxy.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.Code)
	}
	return err
}

func runInit(cmd *cobra.Command, args []string) error {
	bin, err := os.Executable()
	if err != nil {
		bin = "remnix"
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	attach := filepath.Join(filepath.Dir(bin), "remnix-attach")
	out, err := shell.Integration(args[0], bin, shell.Options{
		SuggestEnabled:     cfg.Suggest.IsEnabled(),
		SuggestAccept:      cfg.Suggest.AcceptKeys(),
		SuggestMenu:        cfg.Suggest.MenuEnabled(),
		SuggestCompletions: cfg.Suggest.MenuEnabled() && cfg.Suggest.CompletionsEnabled() && !cfg.PtyProxy.IsEnabled(),
		SuggestMenuMax:     cfg.Suggest.MenuLimit(),
		IconTyped:          cfg.IconTyped(),
		IconHistory:        cfg.IconHistory(),
		IconCompletion:     cfg.IconCompletion(),
		PtyProxyEnabled:    cfg.PtyProxy.IsEnabled(),
		AttachBin:          attach,
		ColorAccent:        cfg.UI.Colors.AccentOrDefault(),
		ColorSelect:        cfg.UI.Colors.SelectOrDefault(),
		ColorText:          cfg.UI.Colors.TextOrDefault(),
		ColorMuted:         cfg.UI.Colors.MutedOrDefault(),
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
	if id, err := client.HistoryStart(command, cwd, session, sh); err == nil {
		fmt.Fprintln(cmd.OutOrStdout(), id)
		return nil
	}
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	id, err := history.NewService(history.NewStore(a.DB), history.NewCache(), a.DB.SQL, a.Config.DeviceID, a.EnqueueHistoryCreated, nil).StartCommand(command, cwd, session, sh)
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), id)
	return nil
}

func runHistoryEnd(cmd *cobra.Command, _ []string) error {
	id, _ := cmd.Flags().GetString("id")
	exit, _ := cmd.Flags().GetInt("exit")
	if err := client.HistoryEnd(id, exit); err == nil {
		return nil
	}
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	return history.NewService(history.NewStore(a.DB), history.NewCache(), a.DB.SQL, a.Config.DeviceID, nil, nil).CompleteCommand(id, exit)
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
	if inserted == 0 {
		return nil
	}
	return kickSyncCheckpoint(cmd.Context(), a)
}

func runSuggestCachePurge(cmd *cobra.Command, tools []string, all bool) error {
	if !all && len(tools) == 0 {
		return fmt.Errorf("specify tools or --all")
	}
	store, err := suggestcache.Open(config.SuggestCachePath())
	if err != nil {
		return err
	}
	defer store.Close()
	if all {
		if err := store.PurgeAll(); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "purged suggest cache")
		return nil
	}
	if err := store.Purge(tools...); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "purged %s\n", strings.Join(tools, " "))
	return nil
}

func runSuggestCacheWarmup(cmd *cobra.Command, tools []string, jobs, depth int, status, foreground, worker bool) error {
	if jobs < 1 {
		return fmt.Errorf("--jobs must be at least 1")
	}
	if depth < 0 {
		return fmt.Errorf("--depth must be >= 0")
	}
	if worker {
		if len(tools) == 0 {
			return fmt.Errorf("specify tools to warm")
		}
		return suggestcache.RunWorker(cmd.Context(), tools, suggestcache.WarmupOptions{
			Jobs:  jobs,
			Depth: depth,
			Out:   cmd.OutOrStdout(),
		})
	}
	if status && len(tools) == 0 {
		return followWarmup(cmd)
	}
	if len(tools) == 0 {
		return fmt.Errorf("specify tools to warm, or --status to attach")
	}
	if foreground {
		store, err := suggestcache.Open(config.SuggestCachePath())
		if err != nil {
			return err
		}
		defer store.Close()
		return suggestcache.Warmup(cmd.Context(), store, tools, suggestcache.WarmupOptions{
			Jobs:  jobs,
			Depth: depth,
			Out:   cmd.OutOrStdout(),
		})
	}
	pid, queued, added, err := suggestcache.Start(tools, jobs, depth)
	if err != nil {
		return err
	}
	if queued {
		if len(added) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "already queued or warming pid=%d tools=%s\n", pid, strings.Join(tools, " "))
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "queued %s onto pid=%d\n", strings.Join(added, " "), pid)
		}
		if !status {
			fmt.Fprintln(cmd.OutOrStdout(), "attach: remnix suggest cache warmup --status")
			return nil
		}
		return followWarmup(cmd)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "warmup started pid=%d tools=%s\n", pid, strings.Join(tools, " "))
	if !status {
		fmt.Fprintln(cmd.OutOrStdout(), "attach: remnix suggest cache warmup --status")
		return nil
	}
	return followWarmup(cmd)
}

func followWarmup(cmd *cobra.Command) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return suggestcache.Follow(ctx, cmd.OutOrStdout())
}
