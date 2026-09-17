#+build linux, darwin, freebsd, netbsd, openbsd
package main

import "core:testing"

@(test)
test_byte_buf_drain_no_shift_until_compact :: proc(t: ^testing.T) {
	b: Byte_Buf
	defer buf_destroy(&b)
	testing.expect(t, buf_add(&b, {1, 2, 3, 4, 5}))
	buf_drain(&b, 2)
	testing.expect(t, buf_len(&b) == 3)
	s := buf_slice(&b)
	testing.expect(t, len(s) == 3 && s[0] == 3 && s[2] == 5)
	buf_drain(&b, 3)
	testing.expect(t, buf_len(&b) == 0)
}

@(test)
test_byte_buf_cap :: proc(t: ^testing.T) {
	b: Byte_Buf
	defer buf_destroy(&b)
	big := make([]u8, BUF_MAX + 1)
	defer delete(big)
	testing.expect(t, !buf_add(&b, big))
	testing.expect(t, buf_add(&b, {1}))
}
