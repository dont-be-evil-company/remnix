//go:build unix

package terminal

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func buildHelper(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	out := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", out, "./testdata/"+name)
	cmd.Dir = filepath.Dir(file)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, b)
	}
	return out
}

func genUserInput(n int, seed int) []byte {
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		b[i] = byte(0x20 + (i*31+seed)%95)
	}
	return b
}

func sendFragmented(t *testing.T, conn net.Conn, data []byte, maxChunk int, seed int64) {
	t.Helper()
	if maxChunk < 1 {
		maxChunk = 1
	}
	rng := seed
	next := func(n int) int {
		if n <= 1 {
			return n
		}
		rng = rng*1103515245 + 12345
		if rng < 0 {
			rng = -rng
		}
		return 1 + int(rng%int64(n))
	}
	for len(data) > 0 {
		n := maxChunk
		if seed != 0 {
			n = next(maxChunk)
		}
		if n > len(data) {
			n = len(data)
		}
		if err := WriteFrame(conn, FrameData, data[:n]); err != nil {
			t.Fatal(err)
		}
		data = data[n:]
		runtime.Gosched()
	}
}

func waitRecord(t *testing.T, path string, want []byte, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	var last []byte
	for time.Now().Before(deadline) {
		last, _ = os.ReadFile(path)
		if bytes.Equal(last, want) {
			return
		}
		if len(last) > len(want) {
			t.Fatalf("child got extra bytes: got %d want %d", len(last), len(want))
		}
		time.Sleep(5 * time.Millisecond)
	}
	off := firstDiff(last, want)
	errb, _ := os.ReadFile(path + ".err")
	t.Fatalf("input mismatch got=%d want=%d firstDiff=%d gotByte=%q wantByte=%q rawerr=%q", len(last), len(want), off, snippet(last, off), snippet(want, off), errb)
}

func firstDiff(got, want []byte) int {
	n := len(got)
	if len(want) < n {
		n = len(want)
	}
	for i := 0; i < n; i++ {
		if got[i] != want[i] {
			return i
		}
	}
	return n
}

func snippet(b []byte, off int) string {
	if off < 0 || off >= len(b) {
		return ""
	}
	end := off + 8
	if end > len(b) {
		end = len(b)
	}
	return strconv.Quote(string(b[off:end]))
}

func startTUISession(t *testing.T, bin, record, output string, expect int, extraEnv ...string) (*Session, net.Conn, *frameCollector) {
	t.Helper()
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "wrap.sh")
	script := "#!/bin/sh\nstty -icanon -echo -isig min 1 time 0 2>/dev/null\nexec " + strconv.Quote(bin) + "\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"TERM=xterm-256color",
		"HOME=" + dir,
		"REMNIX_PTY_RECORD=" + record,
		"REMNIX_PTY_OUTPUT=" + output,
		"REMNIX_PTY_EXPECT=" + strconv.Itoa(expect),
	}
	env = append(env, extraEnv...)
	sess, err := startSession("tui", CreateRequest{
		Shell: wrapper,
		Cwd:   dir,
		Cols:  80,
		Rows:  24,
		Env:   env,
	}, server, RPC{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sess.Close)
	col := startFrameCollector(client)
	t.Cleanup(col.stop)
	waitOutputContains(t, col, "READY", 3*time.Second)
	return sess, client, col
}

func waitOutputContains(t *testing.T, col *frameCollector, needle string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if strings.Contains(col.snapshot(), needle) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("missing %q in output %q", needle, trimForLog(col.snapshot()))
}

func TestWriteFullToPTYExact4096(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer ptmx.Close()
	defer tty.Close()
	if _, err := term.MakeRaw(int(tty.Fd())); err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte("x"), 4096)
	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 8192)
		var got []byte
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) && len(got) < len(want) {
			_ = tty.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, _ := tty.Read(buf)
			if n > 0 {
				got = append(got, buf[:n]...)
			}
		}
		done <- got
	}()
	if err := writeFull(ptmx, want, nil); err != nil {
		t.Fatal(err)
	}
	got := <-done
	if !bytes.Equal(got, want) {
		t.Fatalf("pty write/read got %d want %d firstDiff=%d", len(got), len(want), firstDiff(got, want))
	}
}

func TestPTYInputIntegritySizes(t *testing.T) {
	bin := buildHelper(t, "tui_child")
	for _, n := range []int{100, 2048, 4095} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			record := filepath.Join(t.TempDir(), "rec")
			want := genUserInput(n, 1)
			_, client, _ := startTUISession(t, bin, record, "none", len(want))
			sendFragmented(t, client, want, 128, 1)
			waitRecord(t, record, want, 5*time.Second)
		})
	}
}

func TestPTYInputIntegritySplitAcrossFrames(t *testing.T) {
	bin := buildHelper(t, "tui_child")
	record := filepath.Join(t.TempDir(), "rec")
	want := genUserInput(3000, 1)
	_, client, _ := startTUISession(t, bin, record, "none", len(want))
	if err := WriteFrame(client, FrameData, want[:1500]); err != nil {
		t.Fatal(err)
	}
	runtime.Gosched()
	if err := WriteFrame(client, FrameData, want[1500:]); err != nil {
		t.Fatal(err)
	}
	waitRecord(t, record, want, 5*time.Second)
}

func TestPTYInputIntegritySmall(t *testing.T) {
	bin := buildHelper(t, "tui_child")
	record := filepath.Join(t.TempDir(), "rec")
	want := genUserInput(100, 1)
	_, client, _ := startTUISession(t, bin, record, "none", len(want))
	sendFragmented(t, client, want, 100, 0)
	waitRecord(t, record, want, 5*time.Second)
}

func TestPTYInputIntegrityNoOutput(t *testing.T) {
	bin := buildHelper(t, "tui_child")
	record := filepath.Join(t.TempDir(), "rec")
	want := genUserInput(2048, 1)
	_, client, _ := startTUISession(t, bin, record, "none", len(want))
	sendFragmented(t, client, want, 64, 0)
	waitRecord(t, record, want, 5*time.Second)
}

func TestPTYInputIntegrityModerateOutput(t *testing.T) {
	bin := buildHelper(t, "tui_child")
	record := filepath.Join(t.TempDir(), "rec")
	want := genUserInput(8192, 2)
	_, client, _ := startTUISession(t, bin, record, "moderate", len(want))
	sendFragmented(t, client, want, 64, 2)
	waitRecord(t, record, want, 15*time.Second)
}

func TestPTYInputIntegrityHeavyOutput(t *testing.T) {
	bin := buildHelper(t, "tui_child")
	record := filepath.Join(t.TempDir(), "rec")
	want := genUserInput(16384, 3)
	_, client, _ := startTUISession(t, bin, record, "heavy", len(want), "REMNIX_PTY_YIELD=1")
	sendFragmented(t, client, want, 7, 3)
	waitRecord(t, record, want, 15*time.Second)
}

func TestPTYInputIntegrityBurstyOutput(t *testing.T) {
	bin := buildHelper(t, "tui_child")
	record := filepath.Join(t.TempDir(), "rec")
	want := genUserInput(8192, 4)
	_, client, _ := startTUISession(t, bin, record, "burst", len(want), "REMNIX_PTY_CHUNK=2048", "REMNIX_PTY_SEED=4")
	sendFragmented(t, client, want, 1, 0)
	waitRecord(t, record, want, 15*time.Second)
}

func TestPTYInputIntegrityRapidFragmented(t *testing.T) {
	bin := buildHelper(t, "tui_child")
	record := filepath.Join(t.TempDir(), "rec")
	want := genUserInput(32768, 5)
	_, client, _ := startTUISession(t, bin, record, "heavy", len(want), "REMNIX_PTY_YIELD=1")
	sendFragmented(t, client, want, 17, 99)
	waitRecord(t, record, want, 20*time.Second)
}

func TestPTYInputIntegrityMillionBytes(t *testing.T) {
	if testing.Short() {
		t.Skip("million-byte integrity")
	}
	bin := buildHelper(t, "tui_child")
	record := filepath.Join(t.TempDir(), "rec")
	want := genUserInput(2_000_000, 7)
	_, client, _ := startTUISession(t, bin, record, "heavy", len(want), "REMNIX_PTY_YIELD=1", "REMNIX_PTY_CHUNK=1024")
	sendFragmented(t, client, want, 64, 7)
	waitRecord(t, record, want, 60*time.Second)
}

func TestPTYInputNotStarvedByHeavyOutput(t *testing.T) {
	bin := buildHelper(t, "tui_child")
	record := filepath.Join(t.TempDir(), "rec")
	want := genUserInput(256, 8)
	start := time.Now()
	_, client, _ := startTUISession(t, bin, record, "heavy", len(want), "REMNIX_PTY_CHUNK=4096")
	sendFragmented(t, client, want, 8, 8)
	waitRecord(t, record, want, 5*time.Second)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("input starved by output: %s", elapsed)
	}
}

func TestJobControlRestoresForeground(t *testing.T) {
	sess, client := startCatSession(t)
	col := startFrameCollector(client)
	defer col.stop()

	if !sess.fgSkip.Load() {
		t.Fatal("new session should treat the shell as healthy foreground")
	}
	sess.invalidateForeground()
	if sess.fgSkip.Load() {
		t.Fatal("invalidateForeground must force the next key to refresh")
	}
	sess.ensureForeground()
	if !sess.fgSkip.Load() {
		t.Fatal("ensureForeground should cache a healthy shell pgid")
	}
	if err := WriteFrame(client, FrameData, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(col.snapshot(), "hello") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("input after foreground refresh missing; output %q", trimForLog(col.snapshot()))
}
