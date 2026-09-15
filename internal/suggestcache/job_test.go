package suggestcache

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *lockedBuf) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *lockedBuf) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

func TestWriteReadJob(t *testing.T) {
	t.Setenv("REMNIX_DATA_DIR", t.TempDir())
	st := JobStatus{Running: true, PID: 42, Tools: []string{"aws"}, Message: "warming aws", Pages: 3}
	if err := WriteJob(st); err != nil {
		t.Fatal(err)
	}
	got, err := ReadJob()
	if err != nil {
		t.Fatal(err)
	}
	if !got.Running || got.PID != 42 || got.Pages != 3 || got.Message != "warming aws" {
		t.Fatalf("%+v", got)
	}
}

func TestFollowIdle(t *testing.T) {
	t.Setenv("REMNIX_DATA_DIR", t.TempDir())
	t.Setenv("REMNIX_RUNTIME_DIR", t.TempDir())
	var buf bytes.Buffer
	if err := Follow(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no warmup running") {
		t.Fatalf("%q", buf.String())
	}
}

func TestFollowUntilDone(t *testing.T) {
	t.Setenv("REMNIX_DATA_DIR", t.TempDir())
	t.Setenv("REMNIX_RUNTIME_DIR", t.TempDir())
	if err := WriteJob(JobStatus{
		Running:   true,
		PID:       os.Getpid(),
		Tools:     []string{"aws"},
		Message:   "warming aws",
		StartedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	var buf lockedBuf
	go func() { errCh <- Follow(ctx, &buf) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), "warming aws") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := WriteJob(JobStatus{
		Running:    false,
		PID:        os.Getpid(),
		Tools:      []string{"aws"},
		Message:    "aws: warmed 4 pages",
		FinishedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "warming aws") || !strings.Contains(out, "aws: warmed 4 pages") {
		t.Fatalf("%q", out)
	}
}

func TestStartEnqueuesWhenRunning(t *testing.T) {
	t.Setenv("REMNIX_DATA_DIR", t.TempDir())
	t.Setenv("REMNIX_RUNTIME_DIR", t.TempDir())
	if err := writePID(os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if err := WriteJob(JobStatus{Running: true, PID: os.Getpid(), Tool: "gcloud", Message: "warming gcloud"}); err != nil {
		t.Fatal(err)
	}
	pid, queued, added, err := Start([]string{"aws"}, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !queued || pid != os.Getpid() {
		t.Fatalf("queued=%v pid=%d", queued, pid)
	}
	if len(added) != 1 || added[0] != "aws" {
		t.Fatalf("added %v", added)
	}
	st, err := ReadJob()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(st.Queued, " ") != "aws" {
		t.Fatalf("queued %v", st.Queued)
	}
	_, queued, added, err = Start([]string{"aws"}, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !queued || len(added) != 0 {
		t.Fatalf("duplicate enqueue added %v", added)
	}
}

func TestEnqueueSkipsCurrentAndRemaining(t *testing.T) {
	t.Setenv("REMNIX_DATA_DIR", t.TempDir())
	t.Setenv("REMNIX_RUNTIME_DIR", t.TempDir())
	if err := writePID(os.Getpid()); err != nil {
		t.Fatal(err)
	}
	if err := WriteJob(JobStatus{
		Running:   true,
		PID:       os.Getpid(),
		Tool:      "gcloud",
		Remaining: []string{"kubectl"},
	}); err != nil {
		t.Fatal(err)
	}
	_, added, err := Enqueue([]string{"gcloud", "kubectl", "aws"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(added, " ") != "aws" {
		t.Fatalf("added %v", added)
	}
}

func TestPidAliveSelf(t *testing.T) {
	if !pidAlive(os.Getpid()) {
		t.Fatal("self should be alive")
	}
	if pidAlive(0) || pidAlive(-1) {
		t.Fatal("invalid pid")
	}
}

func TestFollowDeadWorkerShowsError(t *testing.T) {
	t.Setenv("REMNIX_DATA_DIR", t.TempDir())
	t.Setenv("REMNIX_RUNTIME_DIR", t.TempDir())
	if err := WriteJob(JobStatus{
		Running: true,
		PID:     999999,
		Tool:    "aws",
		Message: "warming aws",
		Error:   "warmup panic: runtime error: invalid memory address or nil pointer dereference",
	}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	err := Follow(context.Background(), &buf)
	if err == nil || !strings.Contains(err.Error(), "nil pointer") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(buf.String(), "warming aws") {
		t.Fatalf("%q", buf.String())
	}
}

func TestWarmupReportsProgress(t *testing.T) {
	s := testStore(t)
	probe := func(ctx context.Context, argv []string) (string, error) {
		return "Available Commands:\n  storage     Cloud Storage\n", nil
	}
	var msgs []string
	if err := Warmup(context.Background(), s, []string{"tool"}, WarmupOptions{
		Probe: probe,
		Depth: 1,
		OnProgress: func(p Progress) {
			msgs = append(msgs, p.Message)
		},
	}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(msgs, "\n")
	if !strings.Contains(joined, "warming tool") || !strings.Contains(joined, "warmed") {
		t.Fatalf("%q", joined)
	}
	if !strings.Contains(joined, "in flight") || !strings.Contains(joined, "  tool") {
		t.Fatalf("per-page argv missing: %q", joined)
	}
}
