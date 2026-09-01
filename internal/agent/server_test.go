package agent

import (
	"bufio"
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/app"
	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/history"
)

func testService(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(root, "cfg"))
	t.Setenv("SYNCSH_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("SYNCSH_RUNTIME_DIR", filepath.Join(root, "run"))
	cfg := config.Default()
	cfg.DeviceID = "dev1"
	cfg.DeviceName = "test"
	cfg.Sync.Transport = "none"
	off := false
	cfg.Sync.Enabled = &off
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	a, err := app.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	if err := a.EnsureLocalDevice(); err != nil {
		t.Fatal(err)
	}
	return NewService(a)
}

func TestServiceSuggestStartEnd(t *testing.T) {
	svc := testService(t)
	id, err := svc.Start("git status", "/repo", "sess", "zsh")
	if err != nil || id == "" {
		t.Fatalf("start: id=%q err=%v", id, err)
	}
	if _, err := svc.Start("git status --short", "/repo", "sess", "zsh"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Suggest("git st", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if got != "git status --short" && got != "git status" {
		t.Fatalf("suggest=%q", got)
	}
	list, err := svc.SuggestList("git st", "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("expected suggest-list hits")
	}
	if err := svc.End(id, 0); err != nil {
		t.Fatal(err)
	}
	e, ok, err := history.NewStore(svc.app.DB).Get(id)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if e.ExitStatus == nil || *e.ExitStatus != 0 {
		t.Fatalf("exit %+v", e.ExitStatus)
	}
}

func TestListenAndServeRPC(t *testing.T) {
	svc := testService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- ListenAndServe(ctx, svc) }()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if Ping() == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := Ping(); err != nil {
		t.Fatal(err)
	}
	id, err := DialRPC(opStart, "echo hi", "/tmp", "s", "zsh")
	if err != nil || id == "" {
		t.Fatalf("start rpc: %q %v", id, err)
	}
	got, err := DialRPC(opSuggest, "echo", "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if got != "echo hi" {
		t.Fatalf("suggest=%q", got)
	}
	items, err := DialRPCList(opSuggestList, "echo", "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0] != "echo hi" {
		t.Fatalf("suggest-list=%v", items)
	}
	if _, err := DialRPC(opEnd, id, "0"); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not exit")
	}
}

func TestPipeProtocol(t *testing.T) {
	svc := testService(t)
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	errCh := make(chan error, 1)
	go func() { errCh <- Serve(a, a, svc) }()
	if err := writeField(b, opPing); err != nil {
		t.Fatal(err)
	}
	if _, err := readReply(bufio.NewReader(b)); err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("serve did not exit on EOF")
	}
}
