package terminal

import (
	"bytes"
	"fmt"
)

// queryScanner watches PTY output for terminal probes remnix must answer.
// It is not a second VT parser: mode, cursor, and alternate-screen state come
// from the emulator. Group A replies (DA, DSR 5, CSI ? u) are independent of
// shadow-screen position and stay on the PTY fast path. CPR (CSI 6 n) is only
// located here; the parse worker answers it after bytes through that point
// have been applied to the emulator.
type queryScanner struct {
	buf []byte
}

type queryFeed struct {
	replies []byte
	cprEnds []int
}

func csiFinal(c byte) bool {
	return c >= 0x40 && c <= 0x7e
}

func oscTerminated(s []byte) bool {
	n := len(s)
	if n >= 1 && s[n-1] == 0x07 {
		return true
	}
	return n >= 2 && s[n-2] == 0x1b && s[n-1] == '\\'
}

func (q *queryScanner) feed(p []byte) queryFeed {
	prefix := len(q.buf)
	q.buf = append(q.buf, p...)
	var out queryFeed
	buf := q.buf
	pos := 0
	for pos < len(buf) {
		if buf[pos] != 0x1b {
			i := bytes.IndexByte(buf[pos:], 0x1b)
			if i < 0 {
				pos = len(buf)
				break
			}
			pos += i
			continue
		}
		reply, n, cpr := consumeQuery(buf[pos:])
		if n == 0 {
			if len(buf)-pos > 128 {
				pos++
				continue
			}
			break
		}
		end := pos + n
		if cpr {
			endInP := end - prefix
			if endInP < 0 {
				endInP = 0
			}
			if endInP > len(p) {
				endInP = len(p)
			}
			out.cprEnds = append(out.cprEnds, endInP)
		} else if len(reply) > 0 {
			out.replies = append(out.replies, reply...)
		}
		pos = end
	}
	if pos >= len(buf) {
		q.buf = q.buf[:0]
	} else {
		q.buf = append([]byte(nil), buf[pos:]...)
	}
	return out
}

func consumeQuery(s []byte) (reply []byte, consumed int, cpr bool) {
	if len(s) < 2 || s[0] != 0x1b {
		return nil, 0, false
	}
	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			if csiFinal(s[i]) {
				reply, cpr = replyCSI(s[:i+1])
				return reply, i + 1, cpr
			}
		}
		return nil, 0, false
	case ']':
		for i := 2; i < len(s); i++ {
			if oscTerminated(s[:i+1]) {
				return nil, i + 1, false
			}
		}
		return nil, 0, false
	case 'P':
		for i := 2; i < len(s); i++ {
			if oscTerminated(s[:i+1]) {
				return nil, i + 1, false
			}
		}
		return nil, 0, false
	default:
		return nil, 2, false
	}
}

func replyCSI(s []byte) (reply []byte, cpr bool) {
	fin := s[len(s)-1]
	body := s[2 : len(s)-1]
	switch fin {
	case 'n':
		if bytes.Equal(body, []byte("6")) {
			return nil, true
		}
		if bytes.Equal(body, []byte("5")) {
			return []byte("\x1b[0n"), false
		}
	case 'c':
		if len(body) == 0 || bytes.Equal(body, []byte("0")) {
			return []byte("\x1b[?1;2c"), false
		}
		if len(body) > 0 && body[0] == '>' {
			return []byte("\x1b[>0;1c"), false
		}
	case 'u':
		// CSI ? u is the keyboard-protocol query. Attach hides it from the
		// outer emulator so the host does not also reply. Without an answer
		// the inner TUI may push CSI-u and skip the matching pop on exit,
		// leaving the shell reading key reports instead of glyphs.
		if bytes.Equal(body, []byte("?")) {
			return []byte("\x1b[?0u"), false
		}
	}
	return nil, false
}

func formatCPR(rows, cols, cursorRow, cursorCol int) []byte {
	if rows < 1 {
		rows = 1
	}
	if cols < 1 {
		cols = 1
	}
	return []byte(fmt.Sprintf("\x1b[%d;%dR", cprCell(cursorRow, rows), cprCell(cursorCol, cols)))
}

// cprCell converts a 0-indexed emulator coordinate to a 1-indexed CPR cell.
func cprCell(pos, max int) int {
	n := pos + 1
	if n < 1 {
		n = 1
	}
	if max >= 1 && n > max {
		n = max
	}
	return n
}
