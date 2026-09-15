package suggestcache

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/helpparse"
)

// WarmupOptions controls a tree walk that pre-populates the suggest cache.
type WarmupOptions struct {
	Probe      helpparse.ProbeFunc
	PATH       string
	Jobs       int
	Depth      int
	Out        io.Writer
	OnProgress func(Progress)
}

// Progress is one warmup status update for logs and --status.
type Progress struct {
	Tool     string
	Last     string
	Pages    int
	Pending  int
	InFlight int
	Message  string
}

func applyWarmupDefaults(opts *WarmupOptions) {
	if opts.Jobs < 1 {
		opts.Jobs = 4
	}
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.Probe == nil {
		path := opts.PATH
		opts.Probe = func(ctx context.Context, argv []string) (string, error) {
			return helpparse.ProbeWithPATH(ctx, argv, path)
		}
	}
}

// Warmup purges each tool then probes command nodes into the store.
func Warmup(ctx context.Context, store *Store, tools []string, opts WarmupOptions) error {
	if store == nil {
		return fmt.Errorf("suggest cache: closed")
	}
	applyWarmupDefaults(&opts)
	for _, tool := range tools {
		tool = strings.TrimSpace(tool)
		if tool == "" {
			continue
		}
		if err := warmupTool(ctx, store, tool, opts); err != nil {
			return err
		}
	}
	return nil
}

func warmupTool(ctx context.Context, store *Store, tool string, opts WarmupOptions) error {
	applyWarmupDefaults(&opts)
	if err := store.Purge(tool); err != nil {
		return err
	}
	bin := lookPath(tool, opts.PATH)
	meta := Tool{Name: tool, BinPath: bin, WarmedAt: time.Now().Unix()}
	if st, err := os.Stat(bin); err == nil && !st.IsDir() {
		meta.BinMtime = st.ModTime().Unix()
		meta.BinSize = st.Size()
	}
	meta.Version = probeVersion(ctx, bin)
	if err := store.UpsertTool(meta); err != nil {
		return err
	}

	report(opts, Progress{Tool: tool, Message: fmt.Sprintf("warming %s", tool)})
	if err := walkTool(ctx, store, tool, opts); err != nil {
		return err
	}
	return nil
}

func walkTool(ctx context.Context, store *Store, tool string, opts WarmupOptions) error {
	var mu sync.Mutex
	cond := sync.NewCond(&mu)
	pending := [][]string{{tool}}
	visited := map[string]struct{}{helpparse.ArgvKey([]string{tool}): {}}
	active := 0
	done := 0
	var reportMu sync.Mutex
	emit := func(p Progress) {
		reportMu.Lock()
		defer reportMu.Unlock()
		report(opts, p)
	}

	stopWait := context.AfterFunc(ctx, func() {
		mu.Lock()
		cond.Broadcast()
		mu.Unlock()
	})
	defer stopWait()

	var wg sync.WaitGroup
	worker := func() {
		defer wg.Done()
		for {
			mu.Lock()
			for len(pending) == 0 && active > 0 && ctx.Err() == nil {
				cond.Wait()
			}
			if ctx.Err() != nil || (len(pending) == 0 && active == 0) {
				mu.Unlock()
				return
			}
			argv := pending[0]
			pending = pending[1:]
			active++
			mu.Unlock()

			help, err := opts.Probe(ctx, argv)
			var ents []helpparse.Entity
			sum := ""
			if err == nil && help != "" {
				ents = helpparse.Parse(help)
				sum = helpparse.NodeSummary(help)
			}
			_ = store.Save(argv, ents, sum)

			mu.Lock()
			if ctx.Err() == nil && (opts.Depth <= 0 || len(argv) < opts.Depth) {
				for _, e := range ents {
					if e.Kind == helpparse.KindFlag || strings.HasPrefix(e.Name, "-") {
						continue
					}
					if strings.EqualFold(e.Name, "help") {
						continue
					}
					if !helpparse.SafeArg(e.Name) {
						continue
					}
					child := append(append([]string{}, argv...), e.Name)
					key := helpparse.ArgvKey(child)
					if _, ok := visited[key]; ok {
						continue
					}
					visited[key] = struct{}{}
					pending = append(pending, child)
				}
			}
			active--
			done++
			pages := done
			inflight := active
			queued := len(pending)
			last := strings.Join(argv, " ")
			cond.Broadcast()
			mu.Unlock()

			emit(Progress{
				Tool:     tool,
				Last:     last,
				Pages:    pages,
				Pending:  queued,
				InFlight: inflight,
				Message:  fmt.Sprintf("%s: %d pages (%d in flight, %d queued)  %s", tool, pages, inflight, queued, last),
			})
		}
	}

	n := opts.Jobs
	wg.Add(n)
	for i := 0; i < n; i++ {
		go worker()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	emit(Progress{Tool: tool, Pages: done, Message: fmt.Sprintf("%s: warmed %d pages", tool, done)})
	return nil
}

func report(opts WarmupOptions, p Progress) {
	if opts.Out != nil {
		fmt.Fprintln(opts.Out, p.Message)
	}
	if opts.OnProgress != nil {
		opts.OnProgress(p)
	}
}

func lookPath(name, path string) string {
	if strings.ContainsRune(name, os.PathSeparator) {
		return name
	}
	if path == "" {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		return name
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		cand := filepath.Join(dir, name)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return name
}

func probeVersion(ctx context.Context, bin string) string {
	if bin == "" {
		return ""
	}
	cctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "--version")
	cmd.Stdin = nil
	out, _ := cmd.CombinedOutput()
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = strings.TrimSpace(line[:i])
	}
	if len(line) > 200 {
		line = line[:200]
	}
	return line
}
