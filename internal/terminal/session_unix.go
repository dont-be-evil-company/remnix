//go:build unix

package terminal

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
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
	"golang.org/x/sys/unix"
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
	Xpixel  int
	Ypixel  int
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
	parseDone sync.WaitGroup

	ptyInQ       *ptyInputQueue
	ptyWriteDone sync.WaitGroup
	ptyWriteMu   sync.Mutex
	fgSkip       atomic.Bool
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
	ws := &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
		X:    winsizeU16(req.Xpixel),
		Y:    winsizeU16(req.Ypixel),
	}
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
		ID:      id,
		Cols:    cols,
		Rows:    rows,
		Xpixel:  int(winsizeU16(req.Xpixel)),
		Ypixel:  int(winsizeU16(req.Ypixel)),
		screen:  scr,
		cmd:     cmd,
		ptmx:    ptmx,
		ptmxW:   ptmxW,
		ioctlFD: ctlFD,
		conn:    conn,
		done:    make(chan int, 1),
		closed:  make(chan struct{}),
		parseQ:  newParseQueue(parseQueueBudget),
		sizeQ:   newSizeCoalescer(),
		ptyInQ:  newPTYInputQueue(maxPTYInputBytes),
	}
	s.fgSkip.Store(true)
	s.parseDone.Add(1)
	s.resizeDone.Add(1)
	s.ptyWriteDone.Add(1)
	go s.parseLoop()
	go s.resizeLoop()
	go s.pumpOutput()
	go s.ptyWriteLoop()
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
		if s.ptyInQ != nil {
			s.ptyInQ.stop()
		}
		if s.closed != nil {
			close(s.closed)
		}
		s.parseDone.Wait()
		s.resizeDone.Wait()
		s.ptyWriteDone.Wait()
		if s.screen != nil {
			s.screen.Close()
		}
		s.dumpPTYDiag()
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
				_ = s.writeFullPTY(got.replies)
			}
			s.enqueueParse(chunk, got.cprEnds)
			stripped := kbStrip.feed(chunk)
			if !s.overlayActive.Load() {
				_ = s.sendFrame(FrameData, stripped)
			}
			if ptyDiagEnabled() && s.parseQ != nil {
				ptyDiag.parseSaturations.Store(s.parseQ.stats().saturations)
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
}

func (s *Session) parseLoop() {
	defer s.parseDone.Done()
	for {
		item, ok := s.parseQ.popWait()
		if !ok {
			return
		}
		events := s.screen.Write(item.data)
		if item.cpr && !s.overlayActive.Load() && s.ptmxW != nil {
			cx, cy := s.screen.Cursor()
			cols, rows := s.size()
			_ = s.writeFullPTY(formatCPR(rows, cols, cy, cx))
		}
		if hasAltScreenLeft(events) {
			s.resetKeys.Store(true)
			s.afterAlt.Store(true)
			s.invalidateForeground()
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

func (s *Session) pixelSize() (xpixel, ypixel int) {
	if s == nil {
		return 0, 0
	}
	s.sizeMu.Lock()
	xpixel, ypixel = s.Xpixel, s.Ypixel
	s.sizeMu.Unlock()
	return xpixel, ypixel
}

func (s *Session) setSize(cols, rows, xpixel, ypixel int) {
	if s == nil {
		return
	}
	s.sizeMu.Lock()
	s.Cols, s.Rows = cols, rows
	s.Xpixel, s.Ypixel = xpixel, ypixel
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
		ptySetsize(s.ioctlFD, sz.cols, sz.rows, sz.xpixel, sz.ypixel)
	}
	s.ptyMu.Unlock()
	if s.screen != nil {
		s.screen.Resize(sz.cols, sz.rows)
	}
	s.setSize(sz.cols, sz.rows, sz.xpixel, sz.ypixel)
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
				if ptyDiagEnabled() {
					ptyDiag.framesRecv.Add(1)
				}
				if s.resetKeys.Swap(false) {
					s.keyDec.hold = s.keyDec.hold[:0]
				}
				if s.overlayActive.Load() {
					s.sendOverlayKey(payload)
				} else {
					decoded := s.keyDec.feed(payload)
					if ptyDiagEnabled() {
						ptyDiag.decoderRelease.Add(uint64(len(decoded)))
					}
					if err := s.ptyInQ.push(decoded); err != nil {
						if ptyDiagTrace() {
							slog.Warn("pty input queue", "err", err)
						}
						go s.Close()
						return
					}
				}
			case FrameWinch:
				if len(payload) >= 4 {
					cols := int(binary.BigEndian.Uint16(payload[0:2]))
					rows := int(binary.BigEndian.Uint16(payload[2:4]))
					xpixel, ypixel := s.pixelSize()
					if len(payload) >= 8 {
						xpixel = int(binary.BigEndian.Uint16(payload[4:6]))
						ypixel = int(binary.BigEndian.Uint16(payload[6:8]))
					}
					s.sizeQ.note(cols, rows, xpixel, ypixel)
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

func (s *Session) ptyWriteLoop() {
	defer s.ptyWriteDone.Done()
	if s == nil || s.ptyInQ == nil {
		return
	}
	for {
		data, ok := s.ptyInQ.popWait()
		if !ok {
			return
		}
		if len(data) == 0 {
			continue
		}
		s.ensureForeground()
		if err := s.writeFullPTY(data); err != nil {
			if ptyDiagEnabled() {
				ptyDiag.ptyWriteErrors.Add(1)
			}
			go s.Close()
			return
		}
	}
}

func (s *Session) writeFullPTY(p []byte) error {
	if s == nil || len(p) == 0 {
		return nil
	}
	s.ptyMu.Lock()
	fd := s.ioctlFD
	s.ptyMu.Unlock()
	if fd < 0 {
		return io.ErrClosedPipe
	}
	s.ptyWriteMu.Lock()
	defer s.ptyWriteMu.Unlock()
	start := time.Now()
	if ptyDiagEnabled() {
		ptyDiag.ptyWriteStarts.Add(1)
	}
	// unix.Write on the ioctl dup avoids os.File's netpoller. Polling the
	// write side of a PTY master that is also being Read via os.File deadlocks
	// once the kernel input buffer fills (~4095 bytes).
	err := writeFullFD(fd, p, s.closed)
	diagObserveWrite(time.Since(start))
	if err != nil {
		return err
	}
	if ptyDiagEnabled() {
		ptyDiag.ptyWriteComplete.Add(1)
	}
	return nil
}

func writeFullFD(fd int, p []byte, closed <-chan struct{}) error {
	for len(p) > 0 {
		select {
		case <-closed:
			return net.ErrClosed
		default:
		}
		chunk := p
		if len(chunk) > 256 {
			chunk = p[:256]
		}
		n, err := unix.Write(fd, chunk)
		if n > 0 {
			if n < len(chunk) && ptyDiagEnabled() {
				ptyDiag.ptyShortWrites.Add(1)
			}
			p = p[n:]
		}
		if err == nil {
			if n == 0 {
				return io.ErrShortWrite
			}
			continue
		}
		if err == unix.EINTR {
			if ptyDiagEnabled() {
				ptyDiag.ptyEINTR.Add(1)
			}
			continue
		}
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			if ptyDiagEnabled() {
				ptyDiag.ptyEAGAIN.Add(1)
			}
			if werr := waitFDWritable(fd, closed); werr != nil {
				return werr
			}
			continue
		}
		return err
	}
	return nil
}

func waitFDWritable(fd int, closed <-chan struct{}) error {
	pfd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
	for {
		select {
		case <-closed:
			return net.ErrClosed
		default:
		}
		n, err := unix.Poll(pfd, 50)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
	}
}

func (s *Session) dumpPTYDiag() {
	if s == nil || !ptyDiagEnabled() {
		return
	}
	parseSat := uint64(0)
	if s.parseQ != nil {
		parseSat = s.parseQ.stats().saturations
	}
	n := ptyDiag.writeNsCount.Load()
	avg := uint64(0)
	if n > 0 {
		avg = ptyDiag.writeNsSum.Load() / n
	}
	slog.Info("pty diag",
		"session", s.ID,
		"frames_recv", ptyDiag.framesRecv.Load(),
		"decoder_release_bytes", ptyDiag.decoderRelease.Load(),
		"pty_write_starts", ptyDiag.ptyWriteStarts.Load(),
		"pty_write_complete", ptyDiag.ptyWriteComplete.Load(),
		"pty_short_writes", ptyDiag.ptyShortWrites.Load(),
		"pty_eagain", ptyDiag.ptyEAGAIN.Load(),
		"pty_eintr", ptyDiag.ptyEINTR.Load(),
		"pty_write_errors", ptyDiag.ptyWriteErrors.Load(),
		"input_queued_bytes", ptyDiag.inputQueued.Load(),
		"input_max_depth", ptyDiag.inputMaxDepth.Load(),
		"input_saturated", ptyDiag.inputSaturated.Load(),
		"parse_saturations", parseSat,
		"pty_write_ns_avg", avg,
		"pty_write_ns_max", ptyDiag.writeNsMax.Load(),
	)
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

func winsizeU16(n int) uint16 {
	if n < 0 {
		return 0
	}
	if n > 65535 {
		return 65535
	}
	return uint16(n)
}

func ptySetsize(fd, cols, rows, xpixel, ypixel int) {
	if fd < 0 {
		return
	}
	ws := pty.Winsize{
		Rows: winsizeU16(rows),
		Cols: winsizeU16(cols),
		X:    winsizeU16(xpixel),
		Y:    winsizeU16(ypixel),
	}
	_, _, _ = syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
}

func ptyWinsize(fd int) (cols, rows, xpixel, ypixel int, ok bool) {
	if fd < 0 {
		return 0, 0, 0, 0, false
	}
	var ws pty.Winsize
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return 0, 0, 0, 0, false
	}
	return int(ws.Cols), int(ws.Rows), int(ws.X), int(ws.Y), true
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
	s.fgSkip.Store(true)
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
	xpixel, ypixel := s.pixelSize()
	s.ptyMu.Lock()
	c, r, x, y, ok := ptyWinsize(s.ioctlFD)
	if ok && (r != rows || c != cols || x != xpixel || y != ypixel) {
		ptySetsize(s.ioctlFD, cols, rows, xpixel, ypixel)
		if s.screen != nil && (r != rows || c != cols) {
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
		s.refreshForegroundCache()
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
// sits stopped on SIGTTIN. The hot path uses a cache refreshed by
// watchForeground; recovery syscalls run only when that cache is invalid.
// TIOCSPGRP from the daemon is best-effort (the kernel may require a
// controlling tty); SIGCONT still unsticks a stopped shell.
func (s *Session) ensureForeground() {
	if s.fgSkip.Load() {
		return
	}
	s.refreshForegroundCache()
	if s.fgSkip.Load() {
		return
	}
	s.restoreForeground()
}

func (s *Session) refreshForegroundCache() {
	fg, shell, ok := s.foregroundPgid()
	if !ok || fg <= 1 || fg == shell || !pgidDead(fg) {
		s.fgSkip.Store(true)
		return
	}
	s.fgSkip.Store(false)
}

func (s *Session) invalidateForeground() {
	if s != nil {
		s.fgSkip.Store(false)
	}
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
