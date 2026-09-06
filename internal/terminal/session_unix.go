//go:build unix

package terminal

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
	"github.com/mistweaverco/syncsh/internal/ptyproxy"
)

const (
	EnvActive    = "SYNCSH_SESSION_ACTIVE"
	EnvSessionID = "SYNCSH_SESSION_ID"
	EnvControlFD = "SYNCSH_CONTROL_FD"
)

type Session struct {
	ID      string
	Cols    int
	Rows    int
	screen  *Screen
	cmd     *exec.Cmd
	ptmx    *os.File
	ptmxW   *os.File
	conn    net.Conn
	writeMu sync.Mutex
	done    chan int
	closed  chan struct{}
	once    sync.Once

	overlayActive atomic.Bool
	overlayMu     sync.Mutex
	overlayKeys   chan []byte
	overlayWinch  chan Size
	overlayCancel context.CancelFunc
	keyDec        kittyKeyDecoder
}

func startSession(id string, req CreateRequest, conn net.Conn, rpc RPC) (*Session, error) {
	shell := req.Shell
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		return nil, fmt.Errorf("terminal: shell is required")
	}
	cols, rows := req.Cols, req.Rows
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	scr := newScreen(cols, rows)
	cmd := exec.Command(shell)
	_ = rpc
	baseEnv := req.Env
	if len(baseEnv) == 0 {
		baseEnv = os.Environ()
	}
	cmd.Env = childEnv(baseEnv, shell, id, -1)
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		Ctty:    0,
	}
	ws := &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, err
	}
	if cmd.Process != nil {
		setForegroundPTY(ptmx, cmd.Process.Pid)
		_ = cmd.Process.Signal(syscall.SIGWINCH)
	}
	wfd, err := syscall.Dup(int(ptmx.Fd()))
	if err != nil {
		_ = ptmx.Close()
		return nil, err
	}
	ptmxW := os.NewFile(uintptr(wfd), "pty-master-w")
	s := &Session{
		ID:     id,
		Cols:   cols,
		Rows:   rows,
		screen: scr,
		cmd:    cmd,
		ptmx:   ptmx,
		ptmxW:  ptmxW,
		conn:   conn,
		done:   make(chan int, 1),
		closed: make(chan struct{}),
	}
	go s.pump(conn)
	return s, nil
}

func (s *Session) Snapshot() ptyproxy.Snapshot {
	if s.screen == nil {
		return ptyproxy.Snapshot{}
	}
	return s.screen.Snapshot()
}

func (s *Session) Wait() int {
	return <-s.done
}

func (s *Session) Close() {
	s.once.Do(func() {
		s.cancelOverlay()
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Signal(syscall.SIGHUP)
		}
		if s.ptmx != nil {
			_ = s.ptmx.Close()
		}
		if s.ptmxW != nil {
			_ = s.ptmxW.Close()
		}
		if s.closed != nil {
			close(s.closed)
		}
	})
}

func (s *Session) pump(conn net.Conn) {
	defer s.Close()
	parseCh := make(chan []byte, 256)
	go func() {
		defer func() { _ = recover() }()
		for chunk := range parseCh {
			s.screen.Write(chunk)
		}
	}()
	go func() {
		buf := make([]byte, 8192)
		var queries queryScanner
		var altLeave altLeaveWatch
		var kbStrip keyboardModeStripper
		for {
			n, err := s.ptmx.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				if reply := queries.feed(chunk, s.Rows, s.Cols); len(reply) > 0 {
					// Overlay hides PTY output; queued replies would be typed
					// onto the prompt after Ctrl+R (?0u / [?1;2c).
					if !s.overlayActive.Load() {
						_, _ = s.ptmxW.Write(reply)
					}
				}
				stripped := kbStrip.feed(chunk)
				out, leftAlt := altLeave.feed(stripped)
				if leftAlt {
					s.restoreForeground()
				}
				if !s.overlayActive.Load() {
					_ = s.sendFrame(FrameData, out)
				}
				select {
				case parseCh <- chunk:
				default:
				}
			}
			if err != nil {
				break
			}
		}
		close(parseCh)
	}()
	go func() {
		for {
			kind, payload, err := ReadFrame(conn)
			if err != nil {
				break
			}
			switch kind {
			case FrameData:
				if s.overlayActive.Load() {
					s.sendOverlayKey(payload)
				} else {
					_, _ = s.ptmxW.Write(s.keyDec.feed(payload))
				}
			case FrameWinch:
				if len(payload) >= 4 {
					cols := int(binary.BigEndian.Uint16(payload[0:2]))
					rows := int(binary.BigEndian.Uint16(payload[2:4]))
					// nvim can emit a 1x1 WINCH on exit; that would make the
					// next overlay a single blinking cell.
					if cols >= 8 && rows >= 4 {
						_ = pty.Setsize(s.ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
						s.screen.Resize(cols, rows)
						s.Cols, s.Rows = cols, rows
						s.sendOverlayWinch(cols, rows)
					}
				}
			case FrameExit:
				s.Close()
				return
			}
		}
	}()
	code := 0
	if err := s.cmd.Wait(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	s.done <- code
}

func childEnv(env []string, shellPath, sessionID string, controlFD int) []string {
	tmux := envValue(env, "TMUX")
	proxyTmux := envValue(env, ptyproxy.EnvTmux)
	if proxyTmux == "" {
		proxyTmux = tmux
	}
	if proxyTmux == "" {
		proxyTmux = os.Getenv("TMUX")
	}
	filtered := make([]string, 0, len(env)+4)
	for _, e := range env {
		if strings.HasPrefix(e, ptyproxy.EnvSocket+"=") ||
			strings.HasPrefix(e, "SHELL=") ||
			strings.HasPrefix(e, EnvSessionID+"=") ||
			strings.HasPrefix(e, EnvControlFD+"=") ||
			strings.HasPrefix(e, ptyproxy.EnvActive+"=") ||
			strings.HasPrefix(e, EnvActive+"=") ||
			strings.HasPrefix(e, ptyproxy.EnvTmux+"=") {
			continue
		}
		filtered = append(filtered, e)
	}
	out := append(filtered,
		ptyproxy.EnvActive+"=1",
		EnvActive+"=1",
		EnvSessionID+"="+sessionID,
		ptyproxy.EnvTmux+"="+proxyTmux,
		"SHELL="+shellPath,
	)
	if controlFD >= 0 {
		out = append(out, fmt.Sprintf("%s=%d", EnvControlFD, controlFD))
	}
	return out
}

func (s *Session) sendFrame(kind byte, payload []byte) error {
	if s == nil || s.conn == nil {
		return net.ErrClosed
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return WriteFrame(s.conn, kind, payload)
}

func setForegroundPTY(ptmx *os.File, pid int) {
	pgid := int32(pid)
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, ptmx.Fd(), syscall.TIOCSPGRP, uintptr(unsafe.Pointer(&pgid)))
}

func (s *Session) restoreForeground() {
	if s == nil || s.cmd == nil || s.cmd.Process == nil || s.ptmx == nil {
		return
	}
	setForegroundPTY(s.ptmx, s.cmd.Process.Pid)
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return strings.TrimPrefix(e, prefix)
		}
	}
	return ""
}

func socketPair() (local, remote *os.File, err error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(fds[0]), "syncsh-control-local"),
		os.NewFile(uintptr(fds[1]), "syncsh-control-remote"), nil
}
