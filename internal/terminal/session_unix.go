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
	"time"
	"unsafe"

	"github.com/creack/pty"
	"github.com/dont-be-evil-company/remnix/internal/ptyproxy"
)

const (
	EnvActive    = "REMNIX_SESSION_ACTIVE"
	EnvSessionID = "REMNIX_SESSION_ID"
	EnvControlFD = "REMNIX_CONTROL_FD"
)

type Session struct {
	ID      string
	Cols    int
	Rows    int
	screen  *Screen
	cmd     *exec.Cmd
	ptmx    *os.File
	ptmxW   *os.File
	ioctlFD int // dup of the master; ioctls must not call ptmx.Fd() during Read/Close
	ptyMu   sync.Mutex
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
	resetKeys     atomic.Bool
	afterAlt      atomic.Bool
	idleMu        sync.Mutex

	sizeMu     sync.Mutex
	sizeQ      *sizeCoalescer
	resizeDone sync.WaitGroup

	parseQ    *parseQueue
	parseSig  chan struct{}
	parseDone sync.WaitGroup
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
	ctlFD, err := syscall.Dup(int(ptmx.Fd()))
	if err != nil {
		_ = syscall.Close(wfd)
		_ = ptmx.Close()
		return nil, err
	}
	ptmxW := os.NewFile(uintptr(wfd), "pty-master-w")
	s := &Session{
		ID:       id,
		Cols:     cols,
		Rows:     rows,
		screen:   scr,
		cmd:      cmd,
		ptmx:     ptmx,
		ptmxW:    ptmxW,
		ioctlFD:  ctlFD,
		conn:     conn,
		done:     make(chan int, 1),
		closed:   make(chan struct{}),
		parseSig: make(chan struct{}, 1),
		parseQ:   newParseQueue(parseQueueBudget),
		sizeQ:    newSizeCoalescer(),
	}
	s.parseDone.Add(1)
	s.resizeDone.Add(1)
	go s.parseLoop()
	go s.resizeLoop()
	go s.pumpOutput()
	go s.pump(conn)
	go s.watchForeground()
	return s, nil
}

func (s *Session) Snapshot() ptyproxy.Snapshot {
	if s.screen == nil {
		return ptyproxy.Snapshot{}
	}
	s.syncScreen()
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
		s.closePTY()
		if s.parseQ != nil {
			s.parseQ.stop()
		}
		if s.closed != nil {
			close(s.closed)
		}
		s.wakeParse()
		s.parseDone.Wait()
		s.resizeDone.Wait()
		if s.screen != nil {
			s.screen.Close()
		}
	})
}

func (s *Session) closePTY() {
	if s == nil {
		return
	}
	s.ptyMu.Lock()
	defer s.ptyMu.Unlock()
	if s.ptmx != nil {
		_ = s.ptmx.Close()
	}
	if s.ptmxW != nil {
		_ = s.ptmxW.Close()
	}
	if s.ioctlFD >= 0 {
		_ = syscall.Close(s.ioctlFD)
		s.ioctlFD = -1
	}
}

// pumpOutput forwards PTY bytes immediately. Shadow VT parse is asynchronous
// so terminal emulation cannot back-pressure the real PTY. Callers that need
// an exact screen (Snapshot, overlay, CPR) wait on a parser barrier.
func (s *Session) pumpOutput() {
	buf := make([]byte, 8192)
	var queries queryScanner
	var kbStrip keyboardModeStripper
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			got := queries.feed(chunk)
			if len(got.replies) > 0 && !s.overlayActive.Load() {
				// Overlay hides PTY output; queued replies would be typed
				// onto the prompt after Ctrl+R (?0u / [?1;2c).
				_, _ = s.ptmxW.Write(got.replies)
			}
			s.enqueueParse(chunk, got.cprEnds)
			stripped := kbStrip.feed(chunk)
			if !s.overlayActive.Load() {
				_ = s.sendFrame(FrameData, stripped)
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Session) enqueueParse(p []byte, cprEnds []int) {
	if s == nil || s.parseQ == nil || len(p) == 0 {
		return
	}
	start := 0
	for _, end := range cprEnds {
		if end < start {
			continue
		}
		if end > len(p) {
			end = len(p)
		}
		if end > start {
			s.parseQ.enqueue(p[start:end], true)
			start = end
			continue
		}
		s.parseQ.enqueue(nil, true)
		start = end
	}
	if start < len(p) {
		s.parseQ.enqueue(p[start:], false)
	}
	s.wakeParse()
}

func (s *Session) wakeParse() {
	if s == nil || s.parseSig == nil {
		return
	}
	select {
	case s.parseSig <- struct{}{}:
	default:
	}
}

func (s *Session) parseLoop() {
	defer s.parseDone.Done()
	for {
		select {
		case <-s.closed:
			return
		case <-s.parseSig:
			s.drainParse()
		}
	}
}

func (s *Session) drainParse() {
	for {
		item, ok := s.parseQ.pop()
		if !ok {
			return
		}
		events := s.screen.Write(item.data)
		if item.cpr && !s.overlayActive.Load() && s.ptmxW != nil {
			cx, cy := s.screen.Cursor()
			cols, rows := s.size()
			_, _ = s.ptmxW.Write(formatCPR(rows, cols, cy, cx))
		}
		if hasAltScreenLeft(events) {
			s.resetKeys.Store(true)
			s.afterAlt.Store(true)
			if !s.overlayActive.Load() {
				go func() {
					s.sendIdleReset(idleResetOpts{})
					s.restoreForegroundLater()
				}()
			}
		}
		s.parseQ.markParsed(item.seq)
	}
}

func (s *Session) syncScreen() {
	if s == nil || s.parseQ == nil {
		return
	}
	target := s.parseQ.currentSeq()
	s.parseQ.wait(target)
}

func (s *Session) size() (cols, rows int) {
	if s == nil {
		return 1, 1
	}
	s.sizeMu.Lock()
	cols, rows = s.Cols, s.Rows
	s.sizeMu.Unlock()
	return cols, rows
}

func (s *Session) setSize(cols, rows int) {
	if s == nil {
		return
	}
	s.sizeMu.Lock()
	s.Cols, s.Rows = cols, rows
	s.sizeMu.Unlock()
}

func (s *Session) resizeLoop() {
	defer s.resizeDone.Done()
	if s.sizeQ == nil {
		return
	}
	for {
		select {
		case <-s.closed:
			return
		case <-s.sizeQ.sig:
			s.applyPendingSize()
		}
	}
}

func (s *Session) applyPendingSize() {
	sz, ok := s.sizeQ.take()
	if !ok {
		return
	}
	s.ptyMu.Lock()
	if s.ioctlFD >= 0 {
		ptySetsize(s.ioctlFD, sz.cols, sz.rows)
	}
	s.ptyMu.Unlock()
	if s.screen != nil {
		s.screen.Resize(sz.cols, sz.rows)
	}
	s.setSize(sz.cols, sz.rows)
	s.sendOverlayWinch(sz.cols, sz.rows)
}

func (s *Session) pump(conn net.Conn) {
	defer s.Close()
	go func() {
		for {
			kind, payload, err := ReadFrame(conn)
			if err != nil {
				break
			}
			switch kind {
			case FrameData:
				if s.resetKeys.Swap(false) {
					s.keyDec.hold = s.keyDec.hold[:0]
				}
				if s.overlayActive.Load() {
					s.sendOverlayKey(payload)
				} else {
					s.ensureForeground()
					_, _ = s.ptmxW.Write(s.keyDec.feed(payload))
				}
			case FrameWinch:
				if len(payload) >= 4 {
					cols := int(binary.BigEndian.Uint16(payload[0:2]))
					rows := int(binary.BigEndian.Uint16(payload[2:4]))
					s.sizeQ.note(cols, rows)
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
			strings.HasPrefix(e, ptyproxy.EnvTmux+"=") ||
			strings.HasPrefix(e, ptyproxy.EnvTTY+"=") {
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

func (s *Session) sendIdleReset(opts idleResetOpts) {
	if s == nil {
		return
	}
	s.idleMu.Lock()
	defer s.idleMu.Unlock()
	if s.overlayActive.Load() {
		return
	}
	raw := idleReset(s.screen, opts)
	if len(raw) == 0 {
		return
	}
	s.screen.Write(raw)
	_ = s.sendFrame(FrameRaw, raw)
}

func setForegroundPTY(ptmx *os.File, pid int) {
	setForegroundFD(int(ptmx.Fd()), pid)
}

func setForegroundFD(fd, pid int) {
	if fd < 0 || pid <= 0 {
		return
	}
	pgid := int32(pid)
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCSPGRP, uintptr(unsafe.Pointer(&pgid)))
}

func ptySetsize(fd, cols, rows int) {
	if fd < 0 {
		return
	}
	ws := pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)}
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
}

func ptyWinsize(fd int) (cols, rows int, ok bool) {
	if fd < 0 {
		return 0, 0, false
	}
	var ws pty.Winsize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return 0, 0, false
	}
	return int(ws.Cols), int(ws.Rows), true
}

func (s *Session) restoreForeground() {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return
	}
	s.ptyMu.Lock()
	if s.ioctlFD >= 0 {
		setForegroundFD(s.ioctlFD, s.cmd.Process.Pid)
	}
	s.ptyMu.Unlock()
	_ = s.cmd.Process.Signal(syscall.SIGCONT)
}

func (s *Session) restoreForegroundLater() {
	time.Sleep(80 * time.Millisecond)
	if s == nil || s.overlayActive.Load() {
		return
	}
	if !s.foregroundIdle() {
		return
	}
	fg, shell, ok := s.foregroundPgid()
	if ok && fg != shell {
		s.restoreForeground()
	}
	cols, rows := s.size()
	s.ptyMu.Lock()
	c, r, ok := ptyWinsize(s.ioctlFD)
	if ok && (r != rows || c != cols) {
		ptySetsize(s.ioctlFD, cols, rows)
		if s.screen != nil {
			s.screen.Resize(cols, rows)
		}
	}
	s.ptyMu.Unlock()
}

// watchForeground force-leaves the alt screen when the foreground process is
// gone but the emulator is still on the alternate screen (hard-kill left the
// pane in alt).
func (s *Session) watchForeground() {
	tick := time.NewTimer(50 * time.Millisecond)
	defer tick.Stop()
	for {
		tick.Reset(50 * time.Millisecond)
		select {
		case <-s.closed:
			return
		case <-tick.C:
		}
		if s.overlayActive.Load() || !s.screen.IsAltScreen() || !s.foregroundIdle() {
			continue
		}
		select {
		case <-s.closed:
			return
		case <-time.After(120 * time.Millisecond):
		}
		if s.overlayActive.Load() || !s.screen.IsAltScreen() || !s.foregroundIdle() {
			continue
		}
		s.afterAlt.Store(true)
		s.resetKeys.Store(true)
		s.sendIdleReset(idleResetOpts{includeAlt: true})
		s.restoreForeground()
	}
}

func (s *Session) foregroundPgid() (fg, shell int32, ok bool) {
	if s == nil || s.cmd == nil || s.cmd.Process == nil {
		return 0, 0, false
	}
	s.ptyMu.Lock()
	defer s.ptyMu.Unlock()
	if s.ioctlFD < 0 {
		return 0, 0, false
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(s.ioctlFD), syscall.TIOCGPGRP, uintptr(unsafe.Pointer(&fg)))
	if errno != 0 {
		return 0, 0, false
	}
	return fg, int32(s.cmd.Process.Pid), true
}

func pgidDead(fg int32) bool {
	err := syscall.Kill(-int(fg), 0)
	return err != nil && err != syscall.EPERM
}

func (s *Session) foregroundIdle() bool {
	fg, shell, ok := s.foregroundPgid()
	if !ok {
		return false
	}
	if fg <= 1 || fg == shell {
		return true
	}
	return pgidDead(fg)
}

// ensureForeground gives the shell the PTY if the previous foreground process
// group is gone. Otherwise keys and Ctrl+C land on a dead pgid and the shell
// sits stopped on SIGTTIN.
func (s *Session) ensureForeground() {
	fg, shell, ok := s.foregroundPgid()
	if !ok || fg <= 1 || fg == shell || !pgidDead(fg) {
		return
	}
	s.restoreForeground()
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
