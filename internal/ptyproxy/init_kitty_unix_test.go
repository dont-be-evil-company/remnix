//go:build unix

package ptyproxy

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
)

func TestPreambleKittyDirectDoesNotExec(t *testing.T) {
	parent := os.Getpid()
	cases := []struct {
		name string
		env  []string
		wrap bool
		mode string
	}{
		{name: "kitty", env: []string{"KITTY_WINDOW_ID=1", "KITTY_PID=" + strconv.Itoa(parent)}, wrap: false, mode: "kitty"},
		{name: "stale-pid", env: []string{"KITTY_WINDOW_ID=1", "KITTY_PID=999999"}, wrap: true},
		{name: "tmux", env: []string{"KITTY_WINDOW_ID=1", "KITTY_PID=" + strconv.Itoa(parent), "TMUX=/tmp/tmux"}, wrap: true},
		{name: "force", env: []string{"KITTY_WINDOW_ID=1", "KITTY_PID=" + strconv.Itoa(parent), "REMNIX_PTY_PROXY_FORCE=1"}, wrap: true},
		{name: "plain", wrap: true},
		{name: "disable", env: []string{"REMNIX_PTY_PROXY_DISABLE=1"}, wrap: false},
	}
	for _, shell := range []string{"zsh", "bash"} {
		if _, err := exec.LookPath(shell); err != nil {
			t.Logf("skip %s: %v", shell, err)
			continue
		}
		for _, tc := range cases {
			t.Run(shell+"/"+tc.name, func(t *testing.T) {
				mode, wrapped := runPreamble(t, shell, tc.env)
				if wrapped != tc.wrap {
					t.Fatalf("wrapped=%v want %v (mode=%q)", wrapped, tc.wrap, mode)
				}
				if !tc.wrap && mode != tc.mode {
					t.Fatalf("mode=%q want %q", mode, tc.mode)
				}
			})
		}
	}
}

func runPreamble(t *testing.T, shell string, extra []string) (mode string, wrapped bool) {
	t.Helper()
	bin := t.TempDir()
	logPath := filepath.Join(bin, "wrap.log")
	stub := "#!/bin/sh\nprintf 'WRAP\\n' >> " + shellQuote(logPath) + "\nexit 0\n"
	for _, name := range []string{"remnix", "remnix-attach"} {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	body := Preamble(filepath.Join(bin, "remnix"), shell) + "\necho MODE=${REMNIX_PTY_PROXY_MODE-}\necho DONE\nexit\n"
	home := t.TempDir()
	var cmd *exec.Cmd
	switch shell {
	case "zsh":
		if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd = exec.Command("zsh", "-i")
		extra = append([]string{"ZDOTDIR=" + home}, extra...)
	case "bash":
		rc := filepath.Join(home, "bashrc")
		if err := os.WriteFile(rc, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd = exec.Command("bash", "--noprofile", "--rcfile", rc, "-i")
	default:
		t.Fatalf("shell %s", shell)
	}
	cmd.Env = cleanEnv(append([]string{"REMNIX_WRAP_LOG=" + logPath, "HOME=" + home, "TERM=xterm"}, extra...)...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer ptmx.Close()
	var buf bytes.Buffer
	tmp := make([]byte, 4096)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		_ = ptmx.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		n, readErr := ptmx.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if strings.Contains(buf.String(), "DONE") {
			break
		}
		if readErr != nil && !errors.Is(readErr, os.ErrDeadlineExceeded) {
			break
		}
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
	raw, _ := os.ReadFile(logPath)
	wrapped = strings.Contains(string(raw), "WRAP")
	out := buf.String()
	if i := strings.Index(out, "MODE="); i >= 0 {
		rest := out[i+len("MODE="):]
		if nl := strings.IndexAny(rest, "\r\n"); nl >= 0 {
			rest = rest[:nl]
		}
		mode = strings.TrimSpace(rest)
	}
	if !wrapped && !strings.Contains(out, "DONE") {
		t.Fatalf("shell produced no result\n%s", out)
	}
	return mode, wrapped
}

func cleanEnv(extra ...string) []string {
	out := make([]string, 0, len(extra)+8)
	for _, e := range os.Environ() {
		switch {
		case strings.HasPrefix(e, "KITTY_"),
			strings.HasPrefix(e, "REMNIX_"),
			strings.HasPrefix(e, "TMUX="),
			strings.HasPrefix(e, "ZDOTDIR="):
			continue
		default:
			out = append(out, e)
		}
	}
	return append(out, extra...)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
