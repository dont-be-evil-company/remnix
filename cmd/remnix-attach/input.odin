#+build linux, darwin, freebsd, netbsd, openbsd
package main

// Input_Parser is the outer-terminal boundary: it separates emulator-generated
// replies from real user input. Read boundaries have no semantic meaning.
// Unknown and incomplete sequences are forwarded unchanged. A timeout or
// length limit may flush the hold, but must never discard it.
//
// Consumed protocol classes (complete sequences only):
//   CSI I/O focus reports, CPR CSI ... R, kitty/DA queries ?u ?c >c,
//   and complete OSC/DCS on stdin (emulator replies, not keystrokes).

Parse_State :: enum u8 {
	Ground,
	Escape,
	CSI,
	OSC,
	OSC_Escape,
	DCS,
	DCS_Escape,
}

INPUT_HOLD_MAX :: 256

Input_Parser :: struct {
	state:       Parse_State,
	hold:        [INPUT_HOLD_MAX]u8,
	hold_n:      int,
	focus_off:   bool,
	consumed_n:  u64,
	forwarded_n: u64,
	timeout_n:   u64,
	limit_n:     u64,
}

parser_has_hold :: proc(p: ^Input_Parser) -> bool {
	return p.hold_n > 0
}

parser_reset :: proc(p: ^Input_Parser) {
	p.state = .Ground
	p.hold_n = 0
}

parser_hold_push :: proc(p: ^Input_Parser, ch: u8) -> bool {
	if p.hold_n >= len(p.hold) {
		return false
	}
	p.hold[p.hold_n] = ch
	p.hold_n += 1
	return true
}

parser_emit_hold :: proc(p: ^Input_Parser, dst: []u8, o: int) -> int {
	n := p.hold_n
	if n == 0 {
		return o
	}
	copy(dst[o:], p.hold[:n])
	p.forwarded_n += u64(n)
	parser_reset(p)
	return o + n
}

parser_consume_hold :: proc(p: ^Input_Parser) {
	p.consumed_n += u64(p.hold_n)
	if p.hold_n >= 3 && p.hold[0] == 0x1b && p.hold[1] == '[' {
		fin := p.hold[p.hold_n - 1]
		if fin == 'I' || fin == 'O' {
			p.focus_off = true
		}
	}
	parser_reset(p)
}

parser_finish_seq :: proc(p: ^Input_Parser, dst: []u8, o: int) -> int {
	if drop_input_seq(p.hold[:p.hold_n]) {
		parser_consume_hold(p)
		return o
	}
	return parser_emit_hold(p, dst, o)
}

parser_flush :: proc(p: ^Input_Parser, dst: []u8) -> int {
	if p.hold_n == 0 {
		parser_reset(p)
		return 0
	}
	p.timeout_n += 1
	n := parser_emit_hold(p, dst, 0)
	return n
}

parser_feed :: proc(p: ^Input_Parser, src, dst: []u8) -> int {
	o := 0
	i := 0
	n := len(src)
	for i < n {
		if o >= len(dst) {
			break
		}
		ch := src[i]
		reprocess := false
		switch p.state {
		case .Ground:
			if ch != 0x1b {
				dst[o] = ch
				o += 1
				p.forwarded_n += 1
			} else if !parser_hold_push(p, ch) {
				dst[o] = ch
				o += 1
				p.forwarded_n += 1
			} else {
				p.state = .Escape
			}
		case .Escape:
			if !parser_hold_push(p, ch) {
				o = parser_emit_hold(p, dst, o)
				p.limit_n += 1
				reprocess = true
			} else if ch == '[' {
				p.state = .CSI
			} else if ch == ']' {
				p.state = .OSC
			} else if ch == 'P' {
				p.state = .DCS
			} else {
				o = parser_emit_hold(p, dst, o)
			}
		case .CSI:
			if ch < 0x20 {
				o = parser_emit_hold(p, dst, o)
				reprocess = true
			} else if !parser_hold_push(p, ch) {
				o = parser_emit_hold(p, dst, o)
				p.limit_n += 1
				reprocess = true
			} else if csi_final(ch) {
				o = parser_finish_seq(p, dst, o)
			}
		case .OSC:
			if !parser_hold_push(p, ch) {
				o = parser_emit_hold(p, dst, o)
				p.limit_n += 1
				reprocess = true
			} else if ch == 0x1b {
				p.state = .OSC_Escape
			} else if ch == 0x07 {
				o = parser_finish_seq(p, dst, o)
			}
		case .OSC_Escape:
			if !parser_hold_push(p, ch) {
				o = parser_emit_hold(p, dst, o)
				p.limit_n += 1
				reprocess = true
			} else if ch == '\\' {
				o = parser_finish_seq(p, dst, o)
			} else {
				p.state = .OSC
			}
		case .DCS:
			if !parser_hold_push(p, ch) {
				o = parser_emit_hold(p, dst, o)
				p.limit_n += 1
				reprocess = true
			} else if ch == 0x1b {
				p.state = .DCS_Escape
			} else if ch == 0x07 {
				o = parser_finish_seq(p, dst, o)
			}
		case .DCS_Escape:
			if !parser_hold_push(p, ch) {
				o = parser_emit_hold(p, dst, o)
				p.limit_n += 1
				reprocess = true
			} else if ch == '\\' {
				o = parser_finish_seq(p, dst, o)
			} else {
				p.state = .DCS
			}
		}
		if reprocess {
			continue
		}
		i += 1
	}
	if p.hold_n > 0 {
		diag_add(&diag_parser_buffered, u64(p.hold_n))
	}
	return o
}
