//go:build unix

package ptyproxy

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/mistweaverco/syncsh/internal/config"
	"golang.org/x/term"
)

// Run wraps the given shell in a PTY, mirrors bytes to the outer tty, and
// serves screen snapshots on a unix socket. Exits with the child's status.
func Run(shellPath string) error {
	if shellPath == "" {
		shellPath = os.Getenv("SHELL")
	}
	if shellPath == "" {
		return fmt.Errorf("pty-proxy: --shell is required")
	}

	in := os.Stdin
	out := os.Stdout
	if !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
		// nu config.nu and `syncsh init fish | source` often exec us with a
		// pipe on stdin/stdout. /dev/tty is still the outer terminal.
		tin, tout, err := openOuterTTY()
		if err != nil {
			return fmt.Errorf("pty-proxy: stdin and stdout must be a terminal")
		}
		defer tin.Close()
		defer tout.Close()
		in, out = tin, tout
	}

	cols, rows, err := term.GetSize(int(out.Fd()))
	if err != nil {
		cols, rows = 80, 24
	}
	if cols < 1 {
		cols = 1
	}
	if rows < 1 {
		rows = 1
	}

	scr := newShadow(cols, rows)
	sockPath := serveSnapshots(scr)
	defer func() {
		if sockPath != "" {
			_ = os.Remove(sockPath)
		}
	}()

	cmd := exec.Command(shellPath)
	cmd.Env = childEnv(shellPath)
	cmd.Dir, _ = os.Getwd()
	// Ctty is an index into the child's fd table after stdin is the slave
	// (see syscall.SysProcAttr). 0 = stdin. Required so /dev/tty inside
	// nvim/jj is the inner PTY, not the outer terminal the proxy is reading.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}

	ws, err := pty.GetsizeFull(out)
	if err != nil || ws.Rows < 1 || ws.Cols < 1 {
		ws = &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}
	}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return fmt.Errorf("pty-proxy: start: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	// Keyboard-generated SIGINT/TSTP must not kill the proxy. Outer tty is
	// raw, so ^C/^Z are bytes for the inner foreground app (nvim, jj, ...).
	signal.Ignore(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP, syscall.SIGTTIN, syscall.SIGTTOU)

	old, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return fmt.Errorf("pty-proxy: raw mode: %w", err)
	}
	defer func() { _ = term.Restore(int(in.Fd()), old) }()

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			if err := pty.InheritSize(out, ptmx); err != nil {
				continue
			}
			termRows, termCols, err := pty.Getsize(out)
			if err != nil || termCols < 1 || termRows < 1 {
				continue
			}
			scr.Resize(termCols, termRows)
		}
	}()

	// Separate fd so stdin writes never share os.File with the read loop.
	wfd, err := syscall.Dup(int(ptmx.Fd()))
	if err != nil {
		return fmt.Errorf("pty-proxy: dup: %w", err)
	}
	ptmxW := os.NewFile(uintptr(wfd), "pty-master-w")
	defer func() { _ = ptmxW.Close() }()

	go func() {
		_, _ = io.Copy(ptmxW, in)
	}()

	// Atuin parses the shadow VT off the byte pump. Parsing nvim output on
	// the same goroutine that forwards to the terminal fills the PTY and
	// the inner app looks frozen.
	parseCh := make(chan []byte, 256)
	go func() {
		defer func() { _ = recover() }()
		for chunk := range parseCh {
			scr.Write(chunk)
		}
	}()

	buf := make([]byte, 8192)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if _, werr := out.Write(chunk); werr != nil {
				break
			}
			cp := make([]byte, n)
			copy(cp, chunk)
			select {
			case parseCh <- cp:
			default:
			}
		}
		if err != nil {
			break
		}
	}

	if err := cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ExitError{Code: ee.ExitCode()}
		}
		return err
	}
	return nil
}

// ExitError is a child's non-zero exit status. Defers still run; the CLI
// maps it to os.Exit.
type ExitError struct {
	Code int
}

func (e ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

func openOuterTTY() (in, out *os.File, err error) {
	in, err = os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	fd, err := syscall.Dup(int(in.Fd()))
	if err != nil {
		_ = in.Close()
		return nil, nil, err
	}
	return in, os.NewFile(uintptr(fd), "/dev/tty"), nil
}

func childEnv(shellPath string) []string {
	env := os.Environ()
	filtered := env[:0]
	for _, e := range env {
		if strings.HasPrefix(e, EnvSocket+"=") || strings.HasPrefix(e, "SHELL=") {
			continue
		}
		filtered = append(filtered, e)
	}
	sock := config.PtyProxySocketPath()
	return append(filtered,
		EnvActive+"=1",
		EnvSocket+"="+sock,
		EnvTmux+"="+os.Getenv("TMUX"),
		"SHELL="+shellPath,
	)
}

type shadow struct {
	mu  sync.Mutex
	emu *vt.Emulator
}

func newShadow(cols, rows int) *shadow {
	emu := vt.NewEmulator(cols, rows)
	emu.SetScrollbackSize(0)
	return &shadow{emu: emu}
}

func (s *shadow) Write(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.emu.Write(p)
}

func (s *shadow) Resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emu.Resize(cols, rows)
}

func (s *shadow) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return snapshotFrom(s.emu)
}

// streamSnapshot writes the 8-byte geometry header before encoding cells so
// Ctrl+R can place the overlay without waiting on a full-screen snapshot.
func (s *shadow) streamSnapshot(w io.Writer) {
	s.mu.Lock()
	width, height := s.emu.Width(), s.emu.Height()
	pos := s.emu.CursorPosition()
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	s.mu.Unlock()
	_, _ = w.Write(EncodeHeader(Snapshot{
		Rows:      height,
		Cols:      width,
		CursorRow: pos.Y,
		CursorCol: pos.X,
	}))
	s.mu.Lock()
	w2 := s.emu.Width()
	if w2 < 1 {
		w2 = 1
	}
	rows := make([]string, height)
	for y := 0; y < height; y++ {
		if y < s.emu.Height() {
			rows[y] = encodeRow(s.emu, y, w2)
		}
	}
	s.mu.Unlock()
	for _, row := range rows {
		_, _ = w.Write(EncodeRow(row))
	}
}

func snapshotFrom(emu *vt.Emulator) Snapshot {
	w, h := emu.Width(), emu.Height()
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	pos := emu.CursorPosition()
	rows := make([]string, h)
	for y := 0; y < h; y++ {
		rows[y] = encodeRow(emu, y, w)
	}
	return Snapshot{
		Rows:      h,
		Cols:      w,
		CursorRow: pos.Y,
		CursorCol: pos.X,
		RowANSI:   rows,
	}
}

func encodeRow(emu *vt.Emulator, y, w int) string {
	b := make([]byte, 0, w+8)
	styled := false
	havePrev := false
	var prevStyle uv.Style
	for x := 0; x < w; {
		cell := emu.CellAt(x, y)
		if cell == nil || cell.Width == 0 {
			if styled {
				b = append(b, "\x1b[0m"...)
				styled = false
				havePrev = false
			}
			b = append(b, ' ')
			x++
			continue
		}
		content := cell.Content
		if content == "" {
			content = " "
		}
		st := cell.Style
		if st.IsZero() {
			if styled {
				b = append(b, "\x1b[0m"...)
				styled = false
				havePrev = false
			}
			b = append(b, content...)
		} else {
			if !havePrev {
				b = append(b, st.String()...)
			} else if !prevStyle.Equal(&st) {
				b = append(b, st.Diff(&prevStyle)...)
			}
			prevStyle = st
			havePrev = true
			styled = true
			b = append(b, content...)
		}
		if cell.Width > 1 {
			x += cell.Width
		} else {
			x++
		}
	}
	if styled {
		b = append(b, "\x1b[0m"...)
	}
	return string(b)
}

func serveSnapshots(scr *shadow) string {
	path := config.PtyProxySocketPath()
	if err := os.MkdirAll(config.RuntimeDir(), 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "syncsh pty-proxy: socket dir: %v\n", err)
		return ""
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "syncsh pty-proxy: bind %s: %v\n", path, err)
		return ""
	}
	_ = os.Chmod(path, 0o600)
	go func() {
		defer ln.Close()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				scr.streamSnapshot(c)
			}(conn)
		}
	}()
	return path
}
