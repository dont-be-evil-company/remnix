#+build linux, darwin, freebsd, netbsd, openbsd
package main

import "core:c"
import "core:fmt"
import "core:os"
import "core:strings"
import "core:sync"
import "core:sys/posix"
import "core:thread"

FRAME_DATA :: 0
FRAME_WINCH :: 1
FRAME_EXIT :: 2
FRAME_RAW :: 3
BUF_MAX :: 1024 * 1024

when ODIN_OS == .Linux {
	TIOCGWINSZ :: c.ulong(0x5413)
} else {
	TIOCGWINSZ :: c.ulong(0x40087468)
}

Winsize :: struct {
	ws_row:    u16,
	ws_col:    u16,
	ws_xpixel: u16,
	ws_ypixel: u16,
}

when ODIN_OS == .Darwin {
	foreign import libc_extra "system:System"
} else {
	foreign import libc_extra "system:c"
}

@(default_calling_convention = "c")
foreign libc_extra {
	ioctl :: proc(fd: posix.FD, request: c.ulong, arg: rawptr) -> c.int ---
}

got_winch:      b32
io_stop:        b32
need_focus_off: b32
orig_term:      posix.termios
tty_fd:    posix.FD = -1
tty_in:    posix.FD = -1
raw_set:   bool
sock_io:   posix.FD = -1
wake_rd:   posix.FD = -1
wake_wr:   posix.FD = -1
out_wake_rd: posix.FD = -1
out_wake_wr: posix.FD = -1
io_mu:     sync.Mutex
g_sock_out: Byte_Buf
out_hold:  [128]u8
out_hold_n: int
in_parser: Input_Parser

on_winch :: proc "c" (_: posix.Signal) {
	got_winch = true
}

restore_tty :: proc "c" () {
	if raw_set && tty_fd >= 0 {
		fl := posix.fcntl(tty_fd, .GETFL)
		if fl >= 0 {
			flags := transmute(posix.O_Flags)fl
			flags -= {.NONBLOCK}
			posix.fcntl(tty_fd, .SETFL, flags)
		}
		posix.tcsetattr(tty_fd, .TCSANOW, &orig_term)
		raw_set = false
	}
}

make_raw :: proc(fd: posix.FD) -> bool {
	if posix.tcgetattr(fd, &orig_term) != .OK {
		return false
	}
	t := orig_term
	t.c_iflag -= {.IGNBRK, .BRKINT, .PARMRK, .ISTRIP, .INLCR, .IGNCR, .ICRNL, .IXON}
	t.c_oflag -= {.OPOST}
	t.c_lflag -= {.ECHO, .ECHONL, .ICANON, .ISIG, .IEXTEN}
	cflag := transmute(posix.tcflag_t)t.c_cflag
	cflag = (cflag & ~posix.tcflag_t(posix.CS8 | posix.PARENB)) | posix.tcflag_t(posix.CS8)
	t.c_cflag = transmute(posix.CControl_Flags)cflag
	t.c_cc[.VMIN] = 1
	t.c_cc[.VTIME] = 0
	if posix.tcsetattr(fd, .TCSANOW, &t) != .OK {
		return false
	}
	raw_set = true
	return true
}

set_nonblock :: proc(fd: posix.FD) -> bool {
	fl := posix.fcntl(fd, .GETFL)
	if fl < 0 {
		return false
	}
	flags := transmute(posix.O_Flags)fl
	flags += {.NONBLOCK}
	return posix.fcntl(fd, .SETFL, flags) >= 0
}

wake_reader :: proc() {
	io_stop = true
	if wake_wr >= 0 {
		c1: u8 = 1
		posix.write(wake_wr, &c1, 1)
	}
}

wake_writer :: proc() {
	if out_wake_wr >= 0 {
		c1: u8 = 1
		posix.write(out_wake_wr, &c1, 1)
	}
}

queue_frame :: proc(b: ^Byte_Buf, kind: u8, payload: []u8) -> bool {
	n := u32(len(payload))
	hdr := [5]u8{kind, u8(n >> 24), u8(n >> 16), u8(n >> 8), u8(n)}
	if !buf_add(b, hdr[:]) {
		return false
	}
	if n > 0 && !buf_add(b, payload) {
		return false
	}
	return true
}

// Enqueue a frame for the single socket-writer thread. Never waits for
// writability and never drops keyboard bytes: exhaustion stops the session.
queue_input :: proc(kind: u8, payload: []u8) -> bool {
	sync.mutex_lock(&io_mu)
	ok := queue_frame(&g_sock_out, kind, payload)
	qlen := buf_len(&g_sock_out)
	sync.mutex_unlock(&io_mu)
	if !ok {
		diag_add(&diag_queue_sat, 1)
		io_stop = true
		wake_writer()
		wake_reader()
		return false
	}
	diag_add(&diag_enqueue, u64(len(payload)))
	diag_max(&diag_queue_max, u64(qlen))
	wake_writer()
	return true
}

drain_pipe :: proc(fd: posix.FD) {
	tmp: [64]u8
	for {
		r := posix.read(fd, &tmp[0], c.size_t(len(tmp)))
		if r <= 0 {
			break
		}
	}
}

// Sole writer of framed bytes to the daemon socket. Polls for writability
// without holding io_mu.
sock_to_daemon :: proc() {
	for !io_stop {
		sync.mutex_lock(&io_mu)
		pending := buf_len(&g_sock_out) > 0
		sync.mutex_unlock(&io_mu)
		if !pending {
			pfd := posix.pollfd {
				fd     = out_wake_rd,
				events = {.IN},
			}
			n: posix.nfds_t = out_wake_rd >= 0 ? 1 : 0
			if n == 0 {
				break
			}
			pr := posix.poll(&pfd, n, -1)
			if pr < 0 && posix.errno() != .EINTR {
				break
			}
			if out_wake_rd >= 0 {
				drain_pipe(out_wake_rd)
			}
			continue
		}
		sync.mutex_lock(&io_mu)
		ok := true
		if sock_io >= 0 {
			ok = buf_flush(sock_io, &g_sock_out)
		}
		still := buf_len(&g_sock_out) > 0
		sync.mutex_unlock(&io_mu)
		if !ok {
			io_stop = true
			break
		}
		if still && sock_io >= 0 {
			pfd := posix.pollfd {
				fd     = sock_io,
				events = {.OUT},
			}
			if posix.poll(&pfd, 1, 150) < 0 && posix.errno() != .EINTR {
				break
			}
		}
	}
}

tty_to_sock :: proc() {
	buf: [4096]u8
	filtered: [4096 + INPUT_HOLD_MAX]u8
	for !io_stop {
		pfd: [2]posix.pollfd
		pfd[0] = {fd = tty_in, events = {.IN}}
		pfd[1] = {fd = wake_rd, events = {.IN}}
		n: posix.nfds_t = wake_rd >= 0 ? 2 : 1
		timeout: i32 = -1
		if parser_has_hold(&in_parser) {
			timeout = 16
		}
		pr := posix.poll(&pfd[0], n, timeout)
		if pr < 0 {
			if posix.errno() == .EINTR {
				continue
			}
			if io_stop {
				break
			}
			continue
		}
		if pr == 0 {
			fn := parser_flush(&in_parser, filtered[:])
			diag_add(&diag_timeout_flush, 1)
			if fn > 0 {
				diag_add(&diag_parser_release, u64(fn))
				queue_input(FRAME_DATA, filtered[:fn])
			}
			continue
		}
		if n > 1 && pfd[1].revents & {.IN, .HUP, .ERR, .NVAL} != {} {
			break
		}
		if .IN in pfd[0].revents {
			r := posix.read(tty_in, &buf[0], c.size_t(len(buf)))
			if r > 0 {
				diag_add(&diag_tty_read, u64(r))
				fn := parser_feed(&in_parser, buf[:r], filtered[:])
				if in_parser.focus_off {
					need_focus_off = true
					in_parser.focus_off = false
				}
				diag_add(&diag_protocol_consumed, in_parser.consumed_n)
				in_parser.consumed_n = 0
				if fn > 0 {
					diag_add(&diag_parser_release, u64(fn))
					queue_input(FRAME_DATA, filtered[:fn])
				}
			}
		}
	}
}

write_all :: proc(fd: posix.FD, buf: []u8) -> bool {
	p := buf
	for len(p) > 0 {
		w := posix.write(fd, raw_data(p), c.size_t(len(p)))
		if w < 0 {
			if posix.errno() == .EINTR {
				continue
			}
			return false
		}
		p = p[w:]
	}
	return true
}

read_all :: proc(fd: posix.FD, buf: []u8) -> bool {
	p := buf
	for len(p) > 0 {
		r := posix.read(fd, raw_data(p), c.size_t(len(p)))
		if r < 0 {
			if posix.errno() == .EINTR {
				continue
			}
			return false
		}
		if r == 0 {
			return false
		}
		p = p[r:]
	}
	return true
}

frame_len :: proc(p: []u8, off: int) -> (n: u32, ok: bool) {
	if off + 5 > len(p) {
		return 0, false
	}
	n = u32(p[off + 1]) << 24 | u32(p[off + 2]) << 16 | u32(p[off + 3]) << 8 | u32(p[off + 4])
	if n > 4 * 1024 * 1024 || off + 5 + int(n) > len(p) {
		return 0, false
	}
	return n, true
}

frame_exit_at :: proc(p: []u8, off: int, exit_code: ^int) -> bool {
	n, ok := frame_len(p, off)
	if !ok || p[off] != FRAME_EXIT {
		return false
	}
	if n > 0 {
		exit_code^ = int(p[off + 5])
	}
	return true
}

csi_final :: proc(c: u8) -> bool {
	return c >= 0x40 && c <= 0x7e
}

osc_end :: proc(s: []u8) -> bool {
	n := len(s)
	if n >= 1 && s[n - 1] == 0x07 {
		return true
	}
	return n >= 2 && s[n - 2] == 0x1b && s[n - 1] == '\\'
}

swallow_output_seq :: proc(s: []u8) -> bool {
	if len(s) < 2 || s[0] != 0x1b {
		return false
	}
	if s[1] == '[' {
		fin := s[len(s) - 1]
		// The daemon answers DA/CPR/DSR on the PTY. Do not inject here:
		// replies that miss fish's waiter are typed onto the prompt
		// ([?1;2c). Hide the probes from kitty so it does not also reply.
		if len(s) == 4 && s[2] == '6' && fin == 'n' {
			return true
		}
		if len(s) == 4 && s[2] == '5' && fin == 'n' {
			return true
		}
		// Forward kitty keyboard push/pop (overlay disable) to the emulator.
		// Only hide the query; answering *u on the PTY typed ?0u after Ctrl+R.
		if fin == 'u' && len(s) >= 4 && s[2] == '?' {
			return true
		}
		if fin == 'q' && len(s) >= 3 && s[2] == '>' {
			return true
		}
		if fin == 'c' {
			return true
		}
		return false
	}
	if s[1] == ']' && osc_end(s) {
		// OSC replies are treated as typing once the line editor is up
		// (ESC is eaten; ]11;rgb:... lands on the prompt).
		if len(s) >= 6 && (string(s[2:6]) == "11;?" || string(s[2:6]) == "10;?") {
			return true
		}
		return false
	}
	if s[1] == 'P' && osc_end(s) {
		if len(s) >= 4 && s[2] == '+' && s[3] == 'q' {
			return true
		}
		return false
	}
	return false
}

token_is_1004 :: proc(t: []u8) -> bool {
	return len(t) == 4 && t[0] == '1' && t[1] == '0' && t[2] == '0' && t[3] == '4'
}

// tmux enables focus reporting as part of a combined DECSET
// (\x1b[?1;1004;2004h). An exact match on ?1004h misses that, Kitty injects
// CSI I/O on pane focus, and tmux then waits until Ctrl+C.
rewrite_focus_tracking :: proc(s, dst: []u8) -> (n: int, drop, focus_off, changed: bool) {
	nseq := len(s)
	if nseq < 5 || s[0] != 0x1b || s[1] != '[' || s[2] != '?' {
		return 0, false, false, false
	}
	fin := s[nseq - 1]
	if fin != 'h' && fin != 'l' {
		return 0, false, false, false
	}
	body := s[3:nseq - 1]
	hit := false
	start := 0
	for i := 0; i <= len(body); i += 1 {
		if i == len(body) || body[i] == ';' {
			if token_is_1004(body[start:i]) {
				hit = true
				break
			}
			start = i + 1
		}
	}
	if !hit {
		return 0, false, false, false
	}
	focus_off = fin == 'h'
	if len(dst) < 4 {
		return 0, true, focus_off, true
	}
	dst[0] = 0x1b
	dst[1] = '['
	dst[2] = '?'
	pos := 3
	first := true
	start = 0
	for i := 0; i <= len(body); i += 1 {
		if i == len(body) || body[i] == ';' {
			tok := body[start:i]
			if !token_is_1004(tok) && len(tok) > 0 {
				if !first {
					if pos >= len(dst) {
						return 0, true, focus_off, true
					}
					dst[pos] = ';'
					pos += 1
				}
				first = false
				if pos + len(tok) >= len(dst) {
					return 0, true, focus_off, true
				}
				copy(dst[pos:], tok)
				pos += len(tok)
			}
			start = i + 1
		}
	}
	if first {
		if fin == 'l' {
			return 0, false, false, false
		}
		return 0, true, focus_off, true
	}
	if pos >= len(dst) {
		return 0, true, focus_off, true
	}
	dst[pos] = fin
	return pos + 1, false, focus_off, true
}

out_byte :: proc(tty_out: ^Byte_Buf, ch: u8) -> bool {
	if out_hold_n == 0 {
		if ch != 0x1b {
			b := ch
			return buf_add(tty_out, []u8{b})
		}
		out_hold[out_hold_n] = ch
		out_hold_n += 1
		return true
	}
	if out_hold_n >= 2 && out_hold[1] == '[' && ch < 0x20 {
		out_hold_n = 0
		return out_byte(tty_out, ch)
	}
	if out_hold_n < len(out_hold) {
		out_hold[out_hold_n] = ch
		out_hold_n += 1
	}
	if out_hold_n == 2 && out_hold[1] != '[' && out_hold[1] != ']' && out_hold[1] != 'P' {
		ok := buf_add(tty_out, out_hold[:out_hold_n])
		out_hold_n = 0
		return ok
	}
	complete := false
	if out_hold_n >= 3 && out_hold[1] == '[' && csi_final(ch) {
		complete = true
	} else if out_hold_n >= 3 && (out_hold[1] == ']' || out_hold[1] == 'P') && osc_end(out_hold[:out_hold_n]) {
		complete = true
	}
	if complete {
		ok := true
		seq := out_hold[:out_hold_n]
		rewritten: [128]u8
		n, drop, focus_off, changed := rewrite_focus_tracking(seq, rewritten[:])
		if changed {
			if !drop {
				ok = buf_add(tty_out, rewritten[:n])
			}
			if ok && focus_off {
				ok = buf_add(tty_out, {0x1b, '[', '?', '1', '0', '0', '4', 'l'})
			}
		} else if !swallow_output_seq(seq) {
			ok = buf_add(tty_out, seq)
		}
		out_hold_n = 0
		return ok
	}
	if out_hold_n >= len(out_hold) {
		ok := buf_add(tty_out, out_hold[:out_hold_n])
		out_hold_n = 0
		return ok
	}
	return true
}

feed_output :: proc(tty_out: ^Byte_Buf, p: []u8) -> bool {
	for ch in p {
		if !out_byte(tty_out, ch) {
			return false
		}
	}
	return true
}

drop_input_seq :: proc(s: []u8) -> bool {
	n := len(s)
	if n >= 3 && s[0] == 0x1b && s[1] == '[' && (s[n - 1] == 'I' || s[n - 1] == 'O') {
		return true
	}
	// DCS/OSC on stdin are emulator replies, never keystrokes.
	if n >= 2 && s[0] == 0x1b && (s[1] == 'P' || s[1] == ']') {
		return true
	}
	if n >= 4 && s[0] == 0x1b && s[1] == '[' && s[n - 1] == 'R' {
		return true
	}
	if n >= 4 && s[0] == 0x1b && s[1] == '[' && s[2] == '?' && (s[n - 1] == 'u' || s[n - 1] == 'c') {
		return true
	}
	if n >= 4 && s[0] == 0x1b && s[1] == '[' && s[2] == '>' && s[n - 1] == 'c' {
		return true
	}
	return false
}

// Drop complete emulator replies. Incomplete sequences are held by Input_Parser
// and flushed unchanged on timeout or limit - never discarded.

take_frames :: proc(in_buf, tty_out: ^Byte_Buf, exit_code: ^int, done: ^bool) -> bool {
	for buf_len(in_buf) >= 5 {
		s := buf_slice(in_buf)
		n := u32(s[1]) << 24 | u32(s[2]) << 16 | u32(s[3]) << 8 | u32(s[4])
		if n > 4 * 1024 * 1024 {
			return false
		}
		if buf_len(in_buf) < 5 + int(n) {
			break
		}
		kind := s[0]
		payload := s[5:][:n]
		if kind == FRAME_EXIT {
			if n > 0 {
				exit_code^ = int(payload[0])
			}
			done^ = true
			return true
		}
		if kind == FRAME_RAW {
			out_hold_n = 0
			if n > 0 && !buf_add(tty_out, payload) {
				return true
			}
			buf_drain(in_buf, 5 + int(n))
			continue
		}
		if kind == FRAME_DATA && n > 0 {
			if !feed_output(tty_out, payload) {
				off := 5 + int(n)
				p := buf_slice(in_buf)
				for off + 5 <= len(p) {
					n2, ok := frame_len(p, off)
					if !ok {
						break
					}
					if frame_exit_at(p, off, exit_code) {
						done^ = true
						return true
					}
					off += 5 + int(n2)
				}
				return true
			}
		}
		buf_drain(in_buf, 5 + int(n))
	}
	return true
}

maybe_winch :: proc(cols, rows: ^u32, force: bool) {
	ws: Winsize
	if ioctl(tty_fd, TIOCGWINSZ, &ws) < 0 || ws.ws_col < 1 || ws.ws_row < 1 {
		got_winch = false
		return
	}
	if !force && !got_winch && u32(ws.ws_col) == cols^ && u32(ws.ws_row) == rows^ {
		return
	}
	got_winch = false
	if u32(ws.ws_col) == cols^ && u32(ws.ws_row) == rows^ {
		return
	}
	cols^ = u32(ws.ws_col)
	rows^ = u32(ws.ws_row)
	payload := [4]u8 {
		u8((cols^ >> 8) & 0xff),
		u8(cols^ & 0xff),
		u8((rows^ >> 8) & 0xff),
		u8(rows^ & 0xff),
	}
	queue_input(FRAME_WINCH, payload[:])
}

cstr_set :: proc(s: cstring) -> bool {
	return s != nil && string(s) != ""
}

sock_path :: proc() -> string {
	if rt := posix.getenv("REMNIX_RUNTIME_DIR"); cstr_set(rt) {
		return fmt.tprintf("%s/terminal.sock", rt)
	}
	if rt := posix.getenv("XDG_RUNTIME_DIR"); cstr_set(rt) {
		return fmt.tprintf("%s/remnix/terminal.sock", rt)
	}
	tmp := posix.getenv("TMPDIR")
	if !cstr_set(tmp) {
		tmp = "/tmp"
	}
	return fmt.tprintf("%s/remnix/terminal.sock", tmp)
}

connect_sock :: proc(path: string) -> posix.FD {
	fd := posix.socket(.UNIX, .STREAM)
	if fd < 0 {
		return -1
	}
	addr: posix.sockaddr_un
	addr.sun_family = .UNIX
	if len(path) >= len(addr.sun_path) {
		posix.close(fd)
		return -1
	}
	copy(addr.sun_path[:], path)
	if posix.connect(fd, (^posix.sockaddr)(&addr), posix.socklen_t(size_of(addr))) != .OK {
		posix.close(fd)
		return -1
	}
	return fd
}

exec_fallback :: proc(shell, remnix: cstring) -> ! {
	restore_tty()
	if wake_rd >= 0 {
		posix.close(wake_rd)
		wake_rd = -1
	}
	if wake_wr >= 0 {
		posix.close(wake_wr)
		wake_wr = -1
	}
	if out_wake_rd >= 0 {
		posix.close(out_wake_rd)
		out_wake_rd = -1
	}
	if out_wake_wr >= 0 {
		posix.close(out_wake_wr)
		out_wake_wr = -1
	}
	if tty_in >= 0 && tty_in != tty_fd {
		posix.close(tty_in)
		tty_in = -1
	}
	if tty_fd >= 0 {
		posix.close(tty_fd)
		tty_fd = -1
	}
	if cstr_set(remnix) {
		posix.execl(remnix, remnix, "pty-proxy", "--shell", shell, nil)
	}
	posix.execl(shell, shell, nil)
	posix.execl("/bin/sh", "sh", nil)
	posix._exit(127)
}

write_create :: proc(sock: posix.FD, shell: string, cols, rows: u32) -> bool {
	cwd_buf: [4096]u8
	cwd_cs := posix.getcwd(raw_data(cwd_buf[:]), len(cwd_buf))
	cwd := cwd_cs != nil ? string(cwd_cs) : ""

	env_count := 0
	for i := 0; posix.environ[i] != nil; i += 1 {
		if !strings.contains(string(posix.environ[i]), "\n") {
			env_count += 1
		}
	}

	header_buf: [8192]u8
	header := fmt.bprintf(
		header_buf[:],
		"CREATE\nshell=%s\ncwd=%s\ncols=%d\nrows=%d\nenv_count=%d\n",
		shell,
		cwd,
		cols,
		rows,
		env_count,
	)
	if len(header) >= len(header_buf) || !write_all(sock, transmute([]u8)header) {
		return false
	}
	for i := 0; posix.environ[i] != nil; i += 1 {
		e := string(posix.environ[i])
		if strings.contains(e, "\n") {
			continue
		}
		if !write_all(sock, transmute([]u8)e) || !write_all(sock, {'\n'}) {
			return false
		}
	}
	return write_all(sock, {'\n'})
}

try_start_daemon :: proc(remnix: cstring) -> bool {
	pid := posix.fork()
	if pid < 0 {
		return false
	}
	if pid == 0 {
		devnull := posix.open("/dev/null", {.RDWR})
		if devnull >= 0 {
			posix.dup2(devnull, 0)
			posix.dup2(devnull, 1)
			posix.dup2(devnull, 2)
			if devnull > 2 {
				posix.close(devnull)
			}
		}
		posix.execl(remnix, remnix, "daemon", nil)
		posix._exit(127)
	}
	return true
}

control_path :: proc() -> string {
	if rt := posix.getenv("REMNIX_RUNTIME_DIR"); cstr_set(rt) {
		return fmt.tprintf("%s/control.sock", rt)
	}
	if rt := posix.getenv("XDG_RUNTIME_DIR"); cstr_set(rt) {
		return fmt.tprintf("%s/remnix/control.sock", rt)
	}
	return "/tmp/remnix/control.sock"
}

control_fd :: proc() -> posix.FD {
	if fd := posix.getenv("REMNIX_CONTROL_FD"); cstr_set(fd) {
		return posix.FD(posix.atoi(fd))
	}
	return connect_sock(control_path())
}

rpc_write_fields :: proc(fd: posix.FD, fields: []string) -> bool {
	for field in fields {
		s := transmute([]u8)field
		if !write_all(fd, s) || !write_all(fd, {0}) {
			return false
		}
	}
	return true
}

rpc_read_field_dyn :: proc(fd: posix.FD) -> (s: string, ok: bool) {
	buf: [dynamic]u8
	defer delete(buf)
	for {
		ch: [1]u8
		if !read_all(fd, ch[:]) {
			return "", false
		}
		if ch[0] == 0 {
			return strings.clone(string(buf[:])), true
		}
		if len(buf) > 1024 * 1024 {
			return "", false
		}
		append(&buf, ch[0])
	}
}

run_rpc :: proc(args: []string) -> int {
	if len(args) < 1 {
		return 2
	}
	fd := control_fd()
	if fd < 0 {
		return 1
	}
	defer posix.close(fd)
	if !rpc_write_fields(fd, args) {
		return 1
	}
	st, ok_st := rpc_read_field_dyn(fd)
	payload, ok_pay := rpc_read_field_dyn(fd)
	if !ok_st || !ok_pay {
		return 1
	}
	if st != "ok" {
		return 1
	}
	if len(payload) > 0 {
		fmt.printf("%s\n", payload)
	}
	return 0
}

usage :: proc() {
	fmt.eprintf("usage: remnix-attach [--shell PATH] [--remnix PATH]\n")
	fmt.eprintf("       remnix-attach --rpc OP [fields...]\n")
}

cstr :: proc(s: string) -> cstring {
	return strings.clone_to_cstring(s, context.temp_allocator)
}

errstr :: proc() -> cstring {
	return posix.strerror(posix.errno())
}

main :: proc() {
	if len(os.args) >= 2 && os.args[1] == "--rpc" {
		os.exit(run_rpc(os.args[2:]))
	}

	shell := "/bin/sh"
	if env := posix.getenv("SHELL"); cstr_set(env) {
		shell = string(env)
	}
	remnix := "remnix"

	for i := 1; i < len(os.args); i += 1 {
		switch os.args[i] {
		case "--shell":
			if i + 1 < len(os.args) {
				i += 1
				shell = os.args[i]
			} else {
				usage()
				os.exit(2)
			}
		case "--remnix":
			if i + 1 < len(os.args) {
				i += 1
				remnix = os.args[i]
			} else {
				usage()
				os.exit(2)
			}
		case "-h", "--help":
			usage()
			os.exit(0)
		case:
			usage()
			os.exit(2)
		}
	}

	shell_c := cstr(shell)
	remnix_c := cstr(remnix)
	diag_init()

	tty_fd = posix.open("/dev/tty", {.RDWR})
	if tty_fd < 0 {
		fmt.eprintf("remnix-attach: /dev/tty: %s\n", errstr())
		exec_fallback(shell_c, remnix_c)
	}
	// Separate open so O_NONBLOCK on writes does not make reads miss keys
	// after a split/unfocus (poll+nonblock on the same tty fd).
	tty_in = posix.open("/dev/tty", {})
	if tty_in < 0 {
		tty_in = posix.dup(tty_fd)
	}
	wake: [2]posix.FD
	if posix.pipe(&wake) == .OK {
		wake_rd = wake[0]
		wake_wr = wake[1]
		set_nonblock(wake_wr)
	}
	out_wake: [2]posix.FD
	if posix.pipe(&out_wake) == .OK {
		out_wake_rd = out_wake[0]
		out_wake_wr = out_wake[1]
		set_nonblock(out_wake_rd)
		set_nonblock(out_wake_wr)
	}
	posix.atexit(restore_tty)

	ws: Winsize
	if ioctl(tty_fd, TIOCGWINSZ, &ws) < 0 || ws.ws_col < 1 || ws.ws_row < 1 {
		ws.ws_col = 80
		ws.ws_row = 24
	}

	path := sock_path()
	sock := connect_sock(path)
	if sock < 0 {
		try_start_daemon(remnix_c)
		for i := 0; i < 120 && sock < 0; i += 1 {
			ts := posix.timespec {
				tv_nsec = 25_000_000,
			}
			posix.nanosleep(&ts, nil)
			sock = connect_sock(path)
		}
	}
	if sock < 0 {
		fmt.eprintf("remnix-attach: connect %s: %s\n", path, errstr())
		exec_fallback(shell_c, remnix_c)
	}

	if !write_create(sock, shell, u32(ws.ws_col), u32(ws.ws_row)) {
		fmt.eprintf("remnix-attach: create session failed\n")
		posix.close(sock)
		exec_fallback(shell_c, remnix_c)
	}

	if !make_raw(tty_fd) {
		fmt.eprintf("remnix-attach: raw mode: %s\n", errstr())
		posix.close(sock)
		exec_fallback(shell_c, remnix_c)
	}
	// Kitty pane focus injects CSI I/O. Disable reporting so split/unfocus
	// cannot stall zsh until Ctrl+C.
	write_all(tty_fd, {0x1b, '[', '?', '1', '0', '0', '4', 'l'})
	if !set_nonblock(tty_fd) || !set_nonblock(tty_in) || !set_nonblock(sock) {
		fmt.eprintf("remnix-attach: nonblock: %s\n", errstr())
		posix.close(sock)
		exec_fallback(shell_c, remnix_c)
	}
	sock_io = sock
	posix.signal(.SIGINT, auto_cast posix.SIG_IGN)
	posix.signal(.SIGQUIT, auto_cast posix.SIG_IGN)
	posix.signal(.SIGTSTP, auto_cast posix.SIG_IGN)
	posix.signal(.SIGTTIN, auto_cast posix.SIG_IGN)
	posix.signal(.SIGTTOU, auto_cast posix.SIG_IGN)
	sa: posix.sigaction_t
	sa.sa_handler = on_winch
	posix.sigaction(posix.Signal(posix.SIGWINCH), &sa, nil)

	reader := thread.create_and_start(tty_to_sock)
	if reader == nil {
		fmt.eprintf("remnix-attach: reader thread: %s\n", errstr())
		posix.close(sock)
		exec_fallback(shell_c, remnix_c)
	}
	writer := thread.create_and_start(sock_to_daemon)
	if writer == nil {
		fmt.eprintf("remnix-attach: writer thread: %s\n", errstr())
		io_stop = true
		wake_reader()
		posix.close(sock)
		exec_fallback(shell_c, remnix_c)
	}

	sock_in: Byte_Buf
	tty_out: Byte_Buf
	cols := u32(ws.ws_col)
	rows := u32(ws.ws_row)
	exit_code := 0
	done := false
	tmp: [4096]u8

	for {
		if io_stop {
			break
		}
		if got_winch {
			maybe_winch(&cols, &rows, true)
		}
		if need_focus_off {
			need_focus_off = false
			if !buf_add(&tty_out, {0x1b, '[', '?', '1', '0', '0', '4', 'l'}) {
				break
			}
		}
		if !buf_flush_tty(tty_fd, &tty_out) {
			break
		}
		if !take_frames(&sock_in, &tty_out, &exit_code, &done) {
			break
		}
		if done {
			buf_flush_tty(tty_fd, &tty_out)
			break
		}

		pfd: [2]posix.pollfd
		np: posix.nfds_t = 1
		pfd[0].fd = sock
		pfd[0].events = {.IN}
		pfd[1].fd = -1
		if buf_len(&tty_out) > 0 {
			pfd[1].fd = tty_fd
			pfd[1].events = {.OUT}
			np = 2
		}
		pr := posix.poll(&pfd[0], np, 150)
		if pr < 0 {
			if posix.errno() == .EINTR {
				continue
			}
			break
		}
		if pr == 0 {
			maybe_winch(&cols, &rows, false)
			continue
		}

		if .IN in pfd[0].revents {
			for {
				r := posix.read(sock, &tmp[0], c.size_t(len(tmp)))
				if r > 0 {
					if !buf_add(&sock_in, tmp[:r]) {
						// Backpressure: stop reading until tty_out drains.
						break
					}
					continue
				}
				if r == 0 {
					done = true
					break
				}
				err := posix.errno()
				if err == .EINTR {
					continue
				}
				if err == .EAGAIN || err == .EWOULDBLOCK {
					break
				}
				done = true
				break
			}
		}
		// Only the daemon socket ending the session. TTY HUP/ERR on split
		// or unfocus must not kill key delivery.
		if pfd[0].revents & {.HUP, .ERR, .NVAL} != {} && .IN not_in pfd[0].revents {
			break
		}
		if done {
			take_frames(&sock_in, &tty_out, &exit_code, &done)
			buf_flush_tty(tty_fd, &tty_out)
			break
		}
	}
	io_stop = true
	wake_writer()
	wake_reader()
	posix.shutdown(sock, .RDWR)
	thread.destroy(reader)
	thread.destroy(writer)
	restore_tty()
	posix.close(sock)
	if wake_rd >= 0 {
		posix.close(wake_rd)
		wake_rd = -1
	}
	if wake_wr >= 0 {
		posix.close(wake_wr)
		wake_wr = -1
	}
	if out_wake_rd >= 0 {
		posix.close(out_wake_rd)
		out_wake_rd = -1
	}
	if out_wake_wr >= 0 {
		posix.close(out_wake_wr)
		out_wake_wr = -1
	}
	if tty_in >= 0 && tty_in != tty_fd {
		posix.close(tty_in)
	}
	posix.close(tty_fd)
	tty_fd = -1
	tty_in = -1
	buf_destroy(&sock_in)
	buf_destroy(&g_sock_out)
	buf_destroy(&tty_out)
	diag_dump()
	os.exit(exit_code)
}
