package suggestcache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/config"
	"github.com/gofrs/flock"
)

var errNotRunning = errors.New("warmup not running")

// JobStatus is the on-disk warmup progress snapshot for --status.
type JobStatus struct {
	Running    bool     `json:"running"`
	PID        int      `json:"pid,omitempty"`
	Tools      []string `json:"tools,omitempty"`
	Tool       string   `json:"tool,omitempty"`
	Last       string   `json:"last,omitempty"`
	Remaining  []string `json:"remaining,omitempty"`
	Queued     []string `json:"queued,omitempty"`
	Pages      int      `json:"pages,omitempty"`
	Pending    int      `json:"pending,omitempty"`
	InFlight   int      `json:"in_flight,omitempty"`
	Message    string   `json:"message,omitempty"`
	StartedAt  int64    `json:"started_at,omitempty"`
	FinishedAt int64    `json:"finished_at,omitempty"`
	Error      string   `json:"error,omitempty"`
}

// Line is the live --status text, including tools waiting behind the current one.
func (st JobStatus) Line() string {
	s := st.Message
	if s == "" {
		if st.Tool != "" {
			s = "warming " + st.Tool
		} else {
			s = "starting"
		}
	}
	var extra []string
	if len(st.Remaining) > 0 {
		extra = append(extra, "next "+strings.Join(st.Remaining, " "))
	}
	if len(st.Queued) > 0 {
		extra = append(extra, "queued "+strings.Join(st.Queued, " "))
	}
	if len(extra) == 0 {
		return s
	}
	return s + "  [" + strings.Join(extra, "; ") + "]"
}

// WriteJob atomically replaces the warmup status file.
func WriteJob(st JobStatus) error {
	path := config.SuggestCacheWarmupStatusPath()
	if err := os.MkdirAll(config.DataDir(), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadJob returns the last warmup status, or a zero value if none exists.
func ReadJob() (JobStatus, error) {
	b, err := os.ReadFile(config.SuggestCacheWarmupStatusPath())
	if err != nil {
		if os.IsNotExist(err) {
			return JobStatus{}, nil
		}
		return JobStatus{}, err
	}
	var st JobStatus
	if err := json.Unmarshal(b, &st); err != nil {
		return JobStatus{}, err
	}
	return st, nil
}

func withJob(fn func(*JobStatus) error) error {
	if err := os.MkdirAll(config.DataDir(), 0o700); err != nil {
		return err
	}
	lk := flock.New(config.SuggestCacheWarmupLockPath())
	if err := lk.Lock(); err != nil {
		return err
	}
	defer func() { _ = lk.Unlock() }()
	st, err := ReadJob()
	if err != nil {
		return err
	}
	if err := fn(&st); err != nil {
		return err
	}
	return WriteJob(st)
}

// RunningPID reports a live warmup worker, if any.
func RunningPID() (int, bool) {
	pid := pidFromFile()
	if pid > 0 && pidAlive(pid) {
		return pid, true
	}
	st, err := ReadJob()
	if err == nil && st.Running && st.PID > 0 && pidAlive(st.PID) {
		return st.PID, true
	}
	return 0, false
}

func pidFromFile() int {
	b, err := os.ReadFile(config.SuggestCacheWarmupPIDPath())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

func writePID(pid int) error {
	if err := os.MkdirAll(config.RuntimeDir(), 0o700); err != nil {
		return err
	}
	return os.WriteFile(config.SuggestCacheWarmupPIDPath(), []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

func removePID(pid int) {
	if pidFromFile() == pid {
		_ = os.Remove(config.SuggestCacheWarmupPIDPath())
	}
}

func containsTool(list []string, tool string) bool {
	for _, t := range list {
		if t == tool {
			return true
		}
	}
	return false
}

func appendUnique(dst []string, tools ...string) []string {
	seen := make(map[string]struct{}, len(dst)+len(tools))
	for _, t := range dst {
		seen[t] = struct{}{}
	}
	for _, t := range tools {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		dst = append(dst, t)
	}
	return dst
}

// Enqueue adds tools to a running warmup. Tools already current, remaining, or
// queued are skipped.
func Enqueue(tools []string) (st JobStatus, added []string, err error) {
	err = withJob(func(cur *JobStatus) error {
		if !jobIsLive(*cur) {
			return errNotRunning
		}
		for _, t := range tools {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if t == cur.Tool || containsTool(cur.Remaining, t) || containsTool(cur.Queued, t) {
				continue
			}
			cur.Queued = append(cur.Queued, t)
			cur.Tools = appendUnique(cur.Tools, t)
			added = append(added, t)
		}
		st = *cur
		return nil
	})
	return st, added, err
}

func takeQueued() []string {
	var out []string
	_ = withJob(func(st *JobStatus) error {
		out = append([]string{}, st.Queued...)
		st.Queued = nil
		return nil
	})
	return out
}

// Start launches a detached warmup worker, or enqueues onto the running one.
func Start(tools []string, jobs, depth int) (pid int, queued bool, added []string, err error) {
	if len(tools) == 0 {
		return 0, false, nil, fmt.Errorf("specify tools to warm")
	}
	if _, ok := RunningPID(); ok {
		st, added, err := Enqueue(tools)
		if err == nil {
			return st.PID, true, added, nil
		}
		if !errors.Is(err, errNotRunning) {
			return 0, false, nil, err
		}
	}
	bin, err := os.Executable()
	if err != nil {
		return 0, false, nil, fmt.Errorf("locate remnix binary: %w", err)
	}
	if err := os.MkdirAll(config.RuntimeDir(), 0o700); err != nil {
		return 0, false, nil, err
	}
	logf, err := os.OpenFile(config.SuggestCacheWarmupLogPath(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, false, nil, err
	}
	args := []string{
		"suggest", "cache", "warmup",
		"--worker",
		"--jobs", strconv.Itoa(jobs),
		"--depth", strconv.Itoa(depth),
	}
	args = append(args, tools...)
	cmd := exec.Command(bin, args...)
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Stdin = nil
	detachCmd(cmd)
	if err := cmd.Start(); err != nil {
		_ = logf.Close()
		return 0, false, nil, err
	}
	_ = logf.Close()
	go func() { _ = cmd.Wait() }()
	pid = cmd.Process.Pid
	_ = writePID(pid)
	_ = WriteJob(JobStatus{
		Running:   true,
		PID:       pid,
		Tools:     append([]string{}, tools...),
		Remaining: append([]string{}, tools[1:]...),
		StartedAt: time.Now().Unix(),
		Message:   "starting",
	})
	return pid, false, nil, nil
}

// RunWorker is the detached warmup process. It writes live status for --status
// and picks up tools enqueued by later `warmup` invocations.
func RunWorker(ctx context.Context, tools []string, opts WarmupOptions) (err error) {
	applyWarmupDefaults(&opts)
	pid := os.Getpid()
	_ = writePID(pid)
	defer removePID(pid)
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("warmup panic: %v", rec)
			if opts.Out != nil {
				fmt.Fprintf(opts.Out, "%s\n%s\n", err, debug.Stack())
			}
			_ = withJob(func(st *JobStatus) error {
				st.Running = false
				st.FinishedAt = time.Now().Unix()
				st.InFlight = 0
				st.Pending = 0
				st.Error = err.Error()
				st.Message = err.Error()
				return nil
			})
		}
	}()

	_ = withJob(func(st *JobStatus) error {
		st.Running = true
		st.PID = pid
		st.Tools = appendUnique(st.Tools, tools...)
		if st.StartedAt == 0 {
			st.StartedAt = time.Now().Unix()
		}
		st.Message = "starting"
		st.Error = ""
		st.FinishedAt = 0
		return nil
	})

	opts.OnProgress = func(p Progress) {
		_ = withJob(func(st *JobStatus) error {
			st.Running = true
			st.PID = pid
			st.Tool = p.Tool
			st.Last = p.Last
			st.Pages = p.Pages
			st.Pending = p.Pending
			st.InFlight = p.InFlight
			st.Message = p.Message
			return nil
		})
	}

	store, err := Open(config.SuggestCachePath())
	if err != nil {
		_ = withJob(func(st *JobStatus) error {
			st.Running = false
			st.FinishedAt = time.Now().Unix()
			st.Error = err.Error()
			st.Message = err.Error()
			return nil
		})
		return err
	}
	defer store.Close()

	remaining := append([]string{}, tools...)
	var runErr error
	for ctx.Err() == nil {
		remaining = appendUnique(remaining, takeQueued()...)
		if len(remaining) == 0 {
			var stop bool
			_ = withJob(func(st *JobStatus) error {
				remaining = appendUnique(remaining, st.Queued...)
				st.Queued = nil
				if len(remaining) > 0 {
					return nil
				}
				stop = true
				st.Remaining = nil
				st.Running = false
				st.FinishedAt = time.Now().Unix()
				st.InFlight = 0
				st.Pending = 0
				if st.Message == "" || strings.HasPrefix(st.Message, "starting") {
					st.Message = "warmup complete"
				}
				return nil
			})
			if stop {
				break
			}
			continue
		}
		tool := remaining[0]
		remaining = remaining[1:]
		rest := append([]string{}, remaining...)
		_ = withJob(func(st *JobStatus) error {
			st.Running = true
			st.PID = pid
			st.Tool = tool
			st.Remaining = rest
			st.Tools = appendUnique(st.Tools, tool)
			st.Tools = appendUnique(st.Tools, rest...)
			st.Pages = 0
			st.Pending = 0
			st.InFlight = 0
			st.Last = ""
			st.Message = "warming " + tool
			return nil
		})
		if err := warmupTool(ctx, store, tool, opts); err != nil {
			runErr = err
			break
		}
	}
	if runErr == nil {
		runErr = ctx.Err()
	}
	if runErr != nil {
		_ = withJob(func(st *JobStatus) error {
			st.Running = false
			st.FinishedAt = time.Now().Unix()
			st.InFlight = 0
			st.Pending = 0
			st.Error = runErr.Error()
			if st.Message == "" {
				st.Message = runErr.Error()
			}
			return nil
		})
	}
	return runErr
}

func jobIsLive(st JobStatus) bool {
	if !st.Running {
		return false
	}
	if st.PID > 0 && !pidAlive(st.PID) {
		return false
	}
	if pid, ok := RunningPID(); ok && (st.PID == 0 || pid == st.PID) {
		return true
	}
	return st.PID > 0 && pidAlive(st.PID)
}

func warmupLogTail() string {
	b, err := os.ReadFile(config.SuggestCacheWarmupLogPath())
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	const max = 20
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	return strings.Join(lines, "\n")
}

// Follow prints live warmup progress until the worker finishes.
func Follow(ctx context.Context, w io.Writer) error {
	if w == nil {
		w = io.Discard
	}
	var last string
	sawLive := false
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	first := true
	for {
		st, err := ReadJob()
		if err != nil {
			return err
		}
		live := jobIsLive(st)
		if live {
			sawLive = true
		}
		line := st.Line()
		if first && !live {
			if st.Running && st.PID > 0 && !pidAlive(st.PID) {
				if line != "" && line != "starting" {
					fmt.Fprintln(w, line)
				}
				if st.Error != "" {
					return fmt.Errorf("%s", st.Error)
				}
				if tail := warmupLogTail(); tail != "" {
					fmt.Fprintln(w, tail)
				}
				fmt.Fprintln(w, "warmup not running")
				return fmt.Errorf("warmup process %d is not running", st.PID)
			}
			if st.Message == "" && st.PID == 0 {
				fmt.Fprintln(w, "no warmup running")
				return nil
			}
			if line != "" && line != "starting" {
				fmt.Fprintln(w, line)
			}
			if st.Error != "" {
				return fmt.Errorf("%s", st.Error)
			}
			return nil
		}
		first = false
		if line != "" && line != last {
			fmt.Fprintln(w, line)
			last = line
		}
		if !live {
			if st.Error != "" {
				return fmt.Errorf("%s", st.Error)
			}
			if st.Running && st.PID > 0 && !pidAlive(st.PID) {
				return fmt.Errorf("warmup process %d is not running", st.PID)
			}
			if sawLive {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				return nil
			}
			return ctx.Err()
		case <-tick.C:
		}
	}
}
