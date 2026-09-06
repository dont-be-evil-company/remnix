package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/client"
	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
	"github.com/mistweaverco/syncsh/internal/protocol"
)

func TestDaemonControlPing(t *testing.T) {
	keyring.Use(keyring.NewMemory())
	dir := t.TempDir()
	t.Setenv("SYNCSH_DATA_DIR", dir)
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(dir, "cfg"))
	t.Setenv("SYNCSH_RUNTIME_DIR", filepath.Join(dir, "run"))
	cfg := config.Default()
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	s := NewServer()
	if err := s.Open(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.listen(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- s.serveControl(ctx) }()
	c, err := client.Dial()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Call(protocol.OpPing, nil, nil); err != nil {
		t.Fatal(err)
	}
	var st protocol.Stats
	if err := c.Call(protocol.OpDaemonStats, nil, &st); err != nil {
		t.Fatal(err)
	}
	if st.PID != os.Getpid() {
		t.Fatalf("pid %d", st.PID)
	}
	var started protocol.HistoryStartRes
	if err := c.Call(protocol.OpHistoryStart, protocol.HistoryStartReq{
		Command: "echo hi", Cwd: dir, Session: "sess", Shell: "zsh",
	}, &started); err != nil {
		t.Fatal(err)
	}
	if started.ID == "" {
		t.Fatal("empty id")
	}
	var sugg string
	if err := c.Call(protocol.OpSuggest, protocol.SuggestReq{Prefix: "echo", Cwd: dir}, &sugg); err != nil {
		t.Fatal(err)
	}
	if sugg != "echo hi" {
		t.Fatalf("suggest=%q", sugg)
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(time.Second):
	}
}

func TestStartingTwiceIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_RUNTIME_DIR", filepath.Join(dir, "run"))
	if err := os.MkdirAll(config.RuntimeDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if Ping() == nil {
		t.Fatal("expected no daemon")
	}
}
