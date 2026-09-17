#+build linux, darwin, freebsd, netbsd, openbsd
package main

import "core:c"
import "core:sys/posix"

Byte_Buf :: struct {
	data: [dynamic]u8,
	head: int,
}

buf_len :: proc(b: ^Byte_Buf) -> int {
	if b.head >= len(b.data) {
		return 0
	}
	return len(b.data) - b.head
}

buf_slice :: proc(b: ^Byte_Buf) -> []u8 {
	if b.head >= len(b.data) {
		return {}
	}
	return b.data[b.head:]
}

buf_compact :: proc(b: ^Byte_Buf) {
	if b.head <= 0 {
		return
	}
	n := buf_len(b)
	if n == 0 {
		clear(&b.data)
		b.head = 0
		return
	}
	copy(b.data[:], b.data[b.head:])
	resize(&b.data, n)
	b.head = 0
}

buf_maybe_compact :: proc(b: ^Byte_Buf) {
	if b.head > 4096 && b.head * 2 > len(b.data) {
		buf_compact(b)
	}
}

buf_add :: proc(b: ^Byte_Buf, src: []u8) -> bool {
	if len(src) == 0 {
		return true
	}
	if buf_len(b) + len(src) > BUF_MAX {
		return false
	}
	buf_maybe_compact(b)
	n := append(&b.data, ..src)
	return n == len(src)
}

buf_drain :: proc(b: ^Byte_Buf, n: int) {
	if n <= 0 {
		return
	}
	if n >= buf_len(b) {
		clear(&b.data)
		b.head = 0
		return
	}
	b.head += n
	buf_maybe_compact(b)
}

buf_destroy :: proc(b: ^Byte_Buf) {
	delete(b.data)
	b.head = 0
}

buf_index :: proc(b: ^Byte_Buf, i: int) -> u8 {
	return b.data[b.head + i]
}

buf_flush :: proc(fd: posix.FD, b: ^Byte_Buf) -> bool {
	for buf_len(b) > 0 {
		s := buf_slice(b)
		w := posix.write(fd, raw_data(s), c.size_t(len(s)))
		if w < 0 {
			err := posix.errno()
			if err == .EINTR {
				diag_add(&diag_sock_eintr, 1)
				continue
			}
			if err == .EAGAIN || err == .EWOULDBLOCK {
				diag_add(&diag_sock_eagain, 1)
				return true
			}
			return false
		}
		if w == 0 {
			return true
		}
		if int(w) < len(s) {
			diag_add(&diag_sock_short, 1)
		}
		diag_add(&diag_sock_write, u64(w))
		buf_drain(b, int(w))
	}
	return true
}

buf_flush_tty :: proc(fd: posix.FD, b: ^Byte_Buf) -> bool {
	for buf_len(b) > 0 {
		s := buf_slice(b)
		w := posix.write(fd, raw_data(s), c.size_t(len(s)))
		if w < 0 {
			err := posix.errno()
			if err == .EINTR {
				continue
			}
			if err == .EAGAIN || err == .EWOULDBLOCK || err == .EIO {
				return true
			}
			return false
		}
		if w == 0 {
			return true
		}
		buf_drain(b, int(w))
	}
	return true
}
