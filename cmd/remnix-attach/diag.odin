#+build linux, darwin, freebsd, netbsd, openbsd
package main

import "core:fmt"
import "core:sync"
import "core:sys/posix"

diag_on: bool
diag_tty_read: u64
diag_parser_release: u64
diag_protocol_consumed: u64
diag_parser_buffered: u64
diag_timeout_flush: u64
diag_limit_flush: u64
diag_enqueue: u64
diag_sock_write: u64
diag_sock_short: u64
diag_sock_eagain: u64
diag_sock_eintr: u64
diag_queue_max: u64
diag_queue_sat: u64

diag_init :: proc() {
	v := posix.getenv("REMNIX_PTY_DIAG")
	if !cstr_set(v) {
		return
	}
	s := string(v)
	diag_on = s != "0" && s != "false" && s != "off"
}

diag_add :: proc(p: ^u64, n: u64) {
	if diag_on {
		sync.atomic_add(p, n)
	}
}

diag_max :: proc(p: ^u64, n: u64) {
	if !diag_on {
		return
	}
	old := sync.atomic_load(p)
	if n > old {
		sync.atomic_store(p, n)
	}
}

diag_dump :: proc() {
	if !diag_on {
		return
	}
	fmt.eprintf(
		"remnix-attach pty diag tty_read=%d parser_release=%d protocol_consumed=%d parser_buffered=%d timeout_flush=%d limit_flush=%d enqueue=%d sock_write=%d sock_short=%d sock_eagain=%d sock_eintr=%d queue_max=%d queue_sat=%d\n",
		sync.atomic_load(&diag_tty_read),
		sync.atomic_load(&diag_parser_release),
		sync.atomic_load(&diag_protocol_consumed),
		sync.atomic_load(&diag_parser_buffered),
		sync.atomic_load(&diag_timeout_flush),
		sync.atomic_load(&diag_limit_flush),
		sync.atomic_load(&diag_enqueue),
		sync.atomic_load(&diag_sock_write),
		sync.atomic_load(&diag_sock_short),
		sync.atomic_load(&diag_sock_eagain),
		sync.atomic_load(&diag_sock_eintr),
		sync.atomic_load(&diag_queue_max),
		sync.atomic_load(&diag_queue_sat),
	)
}
