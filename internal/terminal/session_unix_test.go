//go:build unix

package terminal

import (
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/ptyproxy"
)

func TestChildEnvKeepsClientTmux(t *testing.T) {
	env := childEnv([]string{
		"PATH=/bin",
		"TMUX=/tmp/tmux-1000/default,1,0",
		ptyproxy.EnvTmux + "=/tmp/tmux-1000/default,1,0",
		ptyproxy.EnvActive + "=1",
	}, "/bin/zsh", "sess-1", -1)

	gotTmux := envValue(env, "TMUX")
	gotProxy := envValue(env, ptyproxy.EnvTmux)
	if gotTmux != "/tmp/tmux-1000/default,1,0" {
		t.Fatalf("TMUX=%q", gotTmux)
	}
	if gotProxy != gotTmux {
		t.Fatalf("REMNIX_PTY_PROXY_TMUX=%q want %q", gotProxy, gotTmux)
	}
	if envValue(env, EnvActive) != "1" {
		t.Fatal("expected REMNIX_SESSION_ACTIVE")
	}
	if countPrefix(env, ptyproxy.EnvTmux+"=") != 1 {
		t.Fatalf("duplicate %s in %v", ptyproxy.EnvTmux, env)
	}
}

func TestSessionFishTtyAndCurses(t *testing.T) {
	fish, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish not installed")
	}
	home := t.TempDir()
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	req := CreateRequest{
		Shell: fish,
		Cwd:   home,
		Cols:  80,
		Rows:  24,
		Env: []string{
			"PATH=" + os.Getenv("PATH"),
			"TERM=xterm-256color",
			"HOME=" + home,
			"USER=" + os.Getenv("USER"),
			"LANG=C.UTF-8",
		},
	}
	// Same ExtraFiles path the daemon uses.
	rpc := RPC{Start: func(command, cwd, session, shell string) (string, error) {
		return "id-1", nil
	}}
	sess, err := startSession("test", req, server, rpc)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	got := readFramesUntil(t, client, 2*time.Second, "❯")
	if !strings.Contains(got, "❯") && !strings.Contains(got, "Welcome to fish") {
		t.Fatalf("fish never reached a prompt after terminal queries\n%s", trimForLog(got))
	}
}

func TestSessionAnswersPrimaryDA(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/da.sh"
	body := "#!/bin/sh\nstty -icanon -echo min 1 time 0\nprintf '\\033[0c'\nans=$(dd bs=7 count=1 2>/dev/null)\nprintf 'DA_OK:%s\\n' \"$ans\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	sess, err := startSession("da", CreateRequest{
		Shell: script,
		Cwd:   dir,
		Cols:  80,
		Rows:  24,
		Env:   []string{"PATH=" + os.Getenv("PATH"), "TERM=xterm-256color"},
	}, server, RPC{})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	got := readFramesUntil(t, client, 3*time.Second, "DA_OK:")
	if !strings.Contains(got, "DA_OK:\x1b[?1;2c") {
		t.Fatalf("daemon did not answer DA on the PTY\n%s", trimForLog(got))
	}
}

func TestSessionDSRRoundTrip(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/dsr.sh"
	body := "#!/bin/sh\nprintf '\\033[6n'\nread -r ans\nprintf 'DSR_OK:%s\\n' \"$ans\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	sess, err := startSession("dsr", CreateRequest{
		Shell: script,
		Cwd:   dir,
		Cols:  80,
		Rows:  24,
		Env:   []string{"PATH=" + os.Getenv("PATH"), "TERM=xterm-256color"},
	}, server, RPC{})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	deadline := time.Now().Add(3 * time.Second)
	var got string
	replied := false
	for time.Now().Before(deadline) {
		_ = client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		kind, payload, err := ReadFrame(client)
		if err != nil {
			continue
		}
		if kind != FrameData {
			continue
		}
		got += string(payload)
		if !replied && strings.Contains(got, "\x1b[6n") {
			if err := WriteFrame(client, FrameData, []byte("\x1b[24;80R\n")); err != nil {
				t.Fatal(err)
			}
			replied = true
		}
		if strings.Contains(got, "DSR_OK:") {
			return
		}
	}
	t.Fatalf("DSR reply not delivered (replied=%v)\n%s", replied, trimForLog(got))
}

func readFramesUntil(t *testing.T, conn net.Conn, d time.Duration, needle string) string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(d))
	var b strings.Builder
	for {
		kind, payload, err := ReadFrame(conn)
		if err != nil {
			return b.String()
		}
		if kind != FrameData {
			continue
		}
		b.Write(payload)
		if needle == "" || strings.Contains(b.String(), needle) {
			return b.String()
		}
	}
}

func trimForLog(s string) string {
	s = strings.ReplaceAll(s, "\x1b", "\\e")
	if len(s) > 800 {
		return s[:800]
	}
	return s
}

func countPrefix(env []string, prefix string) int {
	n := 0
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

func TestManagerExitSendsFrame(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not installed")
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	errCh := make(chan error, 1)
	go func() {
		m := NewManager()
		errCh <- m.Accept(server)
	}()
	req := CreateRequest{
		Shell: sh,
		Cwd:   t.TempDir(),
		Cols:  80,
		Rows:  24,
		Env:   []string{"PATH=" + os.Getenv("PATH")},
	}
	if err := WriteCreate(client, req); err != nil {
		t.Fatal(err)
	}
	if err := WriteFrame(client, FrameData, []byte("exit 7\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = client.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		kind, payload, err := ReadFrame(client)
		if err != nil {
			continue
		}
		if kind == FrameExit {
			if len(payload) > 0 && payload[0] != 7 && payload[0] != 0 {
				t.Fatalf("exit status %d", payload[0])
			}
			select {
			case err := <-errCh:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("Accept did not return after FRAME_EXIT")
			}
			return
		}
	}
	t.Fatal("FRAME_EXIT not received after shell exit")
}

func TestOverlayDivertsKeysFromPTY(t *testing.T) {
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat not installed")
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	sess, err := startSession("ov", CreateRequest{
		Shell: cat,
		Cols:  80,
		Rows:  24,
		Env:   []string{"PATH=" + os.Getenv("PATH"), "TERM=xterm-256color"},
	}, server, RPC{})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	col := startFrameCollector(client)
	defer col.stop()
	time.Sleep(50 * time.Millisecond)

	ov, err := sess.BeginOverlay(func(termRows int) int { return 10 })
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if !strings.Contains(col.snapshot(), "\x1b[?2026l") {
		t.Fatalf("overlay must end synchronized output so paint is visible after nvim, got %q", trimForLog(col.snapshot()))
	}
	if strings.Contains(col.snapshot(), "\x1b[?1049l") {
		t.Fatalf("overlay must not leave the alt-screen; 1049l after nvim hides the TUI: %q", trimForLog(col.snapshot()))
	}
	if !strings.Contains(col.snapshot(), "\x1b[=0;1u") {
		t.Fatalf("overlay must disable kitty keyboard without pushing a new stack entry: %q", trimForLog(col.snapshot()))
	}
	before := col.snapshot()

	if err := WriteFrame(client, FrameData, []byte("secret\n")); err != nil {
		t.Fatal(err)
	}
	got := readKeys(t, ov.Keys, 500*time.Millisecond)
	if !strings.Contains(got, "secret") {
		t.Fatalf("overlay keys %q", got)
	}
	time.Sleep(50 * time.Millisecond)
	if leaked := strings.TrimPrefix(col.snapshot(), before); strings.Contains(leaked, "secret") {
		t.Fatalf("overlay key leaked to attach as PTY echo: %q", trimForLog(leaked))
	}

	ov.Close()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(col.snapshot(), "\x1b[?25h") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(col.snapshot(), "\x1b[?25h") {
		t.Fatalf("restore should show the cursor, got %q", trimForLog(col.snapshot()))
	}

	if err := WriteFrame(client, FrameData, []byte("world\n")); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(col.snapshot(), "world") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("after overlay, cat should echo, got %q", trimForLog(col.snapshot()))
}

func TestOverlayHidesPTYOutput(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/tick.sh"
	body := "#!/bin/sh\nprintf 'READY\\n'\nwhile true; do printf 'TICK\\n'; sleep 0.05; done\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	sess, err := startSession("tick", CreateRequest{
		Shell: script,
		Cwd:   dir,
		Cols:  80,
		Rows:  24,
		Env:   []string{"PATH=" + os.Getenv("PATH"), "TERM=xterm-256color"},
	}, server, RPC{})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	col := startFrameCollector(client)
	defer col.stop()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(col.snapshot(), "READY") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(col.snapshot(), "READY") {
		t.Fatal("missing READY")
	}
	deadline = time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if strings.Contains(col.snapshot(), "TICK") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	ov, err := sess.BeginOverlay(func(termRows int) int { return 10 })
	if err != nil {
		t.Fatal(err)
	}
	marked := col.snapshot()
	time.Sleep(200 * time.Millisecond)
	hidden := strings.TrimPrefix(col.snapshot(), marked)
	ov.Close()
	if strings.Contains(hidden, "TICK") {
		t.Fatalf("PTY output forwarded during overlay: %q", trimForLog(hidden))
	}
}

type frameCollector struct {
	mu   sync.Mutex
	buf  strings.Builder
	quit chan struct{}
}

func startFrameCollector(conn net.Conn) *frameCollector {
	c := &frameCollector{quit: make(chan struct{})}
	go func() {
		for {
			select {
			case <-c.quit:
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
			kind, payload, err := ReadFrame(conn)
			if err != nil {
				continue
			}
			if kind == FrameData || kind == FrameRaw {
				c.mu.Lock()
				c.buf.Write(payload)
				c.mu.Unlock()
			}
		}
	}()
	return c
}

func (c *frameCollector) snapshot() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buf.String()
}

func (c *frameCollector) stop() {
	select {
	case <-c.quit:
	default:
		close(c.quit)
	}
}

func readKeys(t *testing.T, r interface{ Read([]byte) (int, error) }, d time.Duration) string {
	t.Helper()
	ch := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 64)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
				if strings.Contains(b.String(), "secret") {
					ch <- b.String()
					return
				}
			}
			if err != nil {
				ch <- b.String()
				return
			}
		}
	}()
	select {
	case s := <-ch:
		return s
	case <-time.After(d):
		return ""
	}
}
