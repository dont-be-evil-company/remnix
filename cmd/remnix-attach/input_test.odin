#+build linux, darwin, freebsd, netbsd, openbsd
package main

import "core:bytes"
import "core:testing"

@(test)
test_parser_ascii :: proc(t: ^testing.T) {
	expect_forward(t, transmute([]u8)string("hello"), transmute([]u8)string("hello"))
}

@(test)
test_parser_hjkl :: proc(t: ^testing.T) {
	expect_forward(t, transmute([]u8)string("hjklhjkl"), transmute([]u8)string("hjklhjkl"))
}

@(test)
test_parser_utf8 :: proc(t: ^testing.T) {
	src := transmute([]u8)string("caféπ")
	expect_forward(t, src, src)
	expect_all_splits(t, src, src, 0)
}

@(test)
test_parser_lone_escape_flush :: proc(t: ^testing.T) {
	p: Input_Parser
	dst: [32]u8
	n := parser_feed(&p, {0x1b}, dst[:])
	testing.expect(t, n == 0)
	testing.expect(t, parser_has_hold(&p))
	n = parser_flush(&p, dst[:])
	testing.expect(t, n == 1 && dst[0] == 0x1b)
	testing.expect(t, !parser_has_hold(&p))
}

@(test)
test_parser_escape_then_key :: proc(t: ^testing.T) {
	src := []u8{0x1b, 'x'}
	expect_forward(t, src, src)
}

@(test)
test_parser_alt_combination :: proc(t: ^testing.T) {
	src := []u8{0x1b, 'f'}
	expect_all_splits(t, src, src, 0)
}

@(test)
test_parser_partial_csi_flush :: proc(t: ^testing.T) {
	p: Input_Parser
	dst: [32]u8
	n := parser_feed(&p, {0x1b, '[', '1', ';'}, dst[:])
	testing.expect(t, n == 0)
	n = parser_flush(&p, dst[:])
	testing.expect(t, n == 4)
	testing.expect(t, bytes.compare(dst[:n], {0x1b, '[', '1', ';'}) == 0)
}

@(test)
test_parser_complete_csi_forward :: proc(t: ^testing.T) {
	src := []u8{0x1b, '[', 'A'}
	expect_forward(t, src, src)
}

@(test)
test_parser_unknown_csi_forward :: proc(t: ^testing.T) {
	src := []u8{0x1b, '[', '9', '9', '9', '~'}
	expect_all_splits(t, src, src, 0)
}

@(test)
test_parser_csi_u_forward :: proc(t: ^testing.T) {
	src := []u8{0x1b, '[', '1', '3', ';', '5', 'u'}
	expect_all_splits(t, src, src, 0)
}

@(test)
test_parser_focus_consumed :: proc(t: ^testing.T) {
	src := []u8{'a', 0x1b, '[', 'I', 'b', 0x1b, '[', 'O', 'c'}
	want := transmute([]u8)string("abc")
	p: Input_Parser
	dst: [32]u8
	n := parser_feed(&p, src, dst[:])
	testing.expect(t, bytes.compare(dst[:n], want) == 0)
	testing.expect(t, p.focus_off)
	testing.expect(t, p.consumed_n == 6)
}

@(test)
test_parser_cpr_consumed :: proc(t: ^testing.T) {
	src := []u8{0x1b, '[', '1', '0', ';', '2', '0', 'R', 'x'}
	expect_parse(t, src, {u8('x')}, 8)
}

@(test)
test_parser_kitty_query_consumed :: proc(t: ^testing.T) {
	src := []u8{0x1b, '[', '?', '0', 'u', 'y'}
	expect_parse(t, src, {u8('y')}, 5)
}

@(test)
test_parser_da_query_consumed :: proc(t: ^testing.T) {
	src := []u8{0x1b, '[', '?', '1', ';', '2', 'c', 'z'}
	expect_parse(t, src, {u8('z')}, 7)
}

@(test)
test_parser_secondary_da_consumed :: proc(t: ^testing.T) {
	src := []u8{0x1b, '[', '>', '0', ';', '9', '5', ';', '0', 'c', 'k'}
	expect_parse(t, src, {u8('k')}, 10)
}

@(test)
test_parser_osc_consumed :: proc(t: ^testing.T) {
	src := []u8{0x1b, ']', '1', '1', ';', '?', 0x07, 'q'}
	expect_parse(t, src, {u8('q')}, 7)
}

@(test)
test_parser_dcs_consumed :: proc(t: ^testing.T) {
	src := []u8{0x1b, 'P', '1', '$', 'r', 0x1b, '\\', 'w'}
	expect_parse(t, src, {u8('w')}, 7)
}

@(test)
test_parser_incomplete_osc_flush :: proc(t: ^testing.T) {
	p: Input_Parser
	dst: [32]u8
	src := []u8{0x1b, ']', '1', '1'}
	n := parser_feed(&p, src, dst[:])
	testing.expect(t, n == 0)
	n = parser_flush(&p, dst[:])
	testing.expect(t, bytes.compare(dst[:n], src) == 0)
}

@(test)
test_parser_malformed_osc_limit :: proc(t: ^testing.T) {
	p: Input_Parser
	src: [INPUT_HOLD_MAX + 8]u8
	src[0] = 0x1b
	src[1] = ']'
	for i := 2; i < len(src); i += 1 {
		src[i] = 'x'
	}
	dst: [INPUT_HOLD_MAX + 16]u8
	n := parser_feed(&p, src[:], dst[:])
	testing.expect(t, n > 0)
	testing.expect(t, p.limit_n > 0)
}

@(test)
test_parser_truncated_then_more :: proc(t: ^testing.T) {
	p: Input_Parser
	dst: [64]u8
	n := parser_feed(&p, {0x1b, '['}, dst[:])
	testing.expect(t, n == 0)
	n = parser_feed(&p, {'A', 'z'}, dst[:])
	testing.expect(t, bytes.compare(dst[:n], {0x1b, '[', 'A', 'z'}) == 0)
}

@(test)
test_parser_c0_flushes_csi :: proc(t: ^testing.T) {
	src := []u8{0x1b, '[', 3, 'a'}
	expect_forward(t, src, src)
}

@(test)
test_parser_multiple_sequences :: proc(t: ^testing.T) {
	src := []u8{'a', 0x1b, '[', 'A', 'b', 0x1b, '[', 'I', 'c'}
	want := []u8{'a', 0x1b, '[', 'A', 'b', 'c'}
	expect_parse(t, src, want, 3)
}

@(test)
test_parser_surrounding_bytes :: proc(t: ^testing.T) {
	src := []u8{'1', 0x1b, '[', 'B', '2'}
	expect_all_splits(t, src, src, 0)
}

@(test)
test_parser_all_splits_csi_u :: proc(t: ^testing.T) {
	src := []u8{0x1b, '[', '1', ';', '5', 'u'}
	expect_all_splits(t, src, src, 0)
}

@(test)
test_parser_all_splits_focus_with_keys :: proc(t: ^testing.T) {
	src := []u8{'h', 0x1b, '[', 'I', 'j'}
	expect_all_splits(t, src, {'h', 'j'}, 3)
}

expect_forward :: proc(t: ^testing.T, src, want: []u8) {
	expect_parse(t, src, want, 0)
	expect_all_splits(t, src, want, 0)
}

expect_parse :: proc(t: ^testing.T, src, want: []u8, consumed: u64) {
	p: Input_Parser
	dst: [4096]u8
	n := parser_feed(&p, src, dst[:])
	if p.hold_n > 0 {
		n += parser_flush(&p, dst[n:])
	}
	testing.expectf(t, bytes.compare(dst[:n], want) == 0, "got %v want %v", dst[:n], want)
	testing.expectf(t, p.consumed_n == consumed, "consumed %d want %d", p.consumed_n, consumed)
}

expect_all_splits :: proc(t: ^testing.T, src, want: []u8, consumed: u64) {
	for split := 0; split <= len(src); split += 1 {
		p: Input_Parser
		dst: [4096]u8
		n := parser_feed(&p, src[:split], dst[:])
		n += parser_feed(&p, src[split:], dst[n:])
		if p.hold_n > 0 {
			n += parser_flush(&p, dst[n:])
		}
		testing.expectf(
			t,
			bytes.compare(dst[:n], want) == 0,
			"split %d got %v want %v",
			split,
			dst[:n],
			want,
		)
		testing.expectf(t, p.consumed_n == consumed, "split %d consumed %d want %d", split, p.consumed_n, consumed)
	}
	p: Input_Parser
	dst: [4096]u8
	n := 0
	for i := 0; i < len(src); i += 1 {
		n += parser_feed(&p, src[i:i + 1], dst[n:])
	}
	if p.hold_n > 0 {
		n += parser_flush(&p, dst[n:])
	}
	testing.expectf(t, bytes.compare(dst[:n], want) == 0, "bytewise got %v want %v", dst[:n], want)
}
