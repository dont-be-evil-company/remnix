package terminal

import (
	"bytes"
	"fmt"
)

// queryScanner watches PTY output for terminal probes and builds stdin replies.
// Fish reads those replies from the PTY slave; answering here avoids the attach
// round-trip that delivered DA after fish's 10s timeout (and then typed [?1;2c).
type queryScanner struct {
	buf []byte
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

func (q *queryScanner) feed(p []byte, rows, cols, cursorRow, cursorCol int) []byte {
	if rows < 1 {
		rows = 1
	}
	if cols < 1 {
		cols = 1
	}
	q.buf = append(q.buf, p...)
	var replies []byte
	for len(q.buf) > 0 {
		if q.buf[0] != 0x1b {
			i := bytes.IndexByte(q.buf, 0x1b)
			if i < 0 {
				q.buf = q.buf[:0]
				break
			}
			q.buf = q.buf[i:]
			continue
		}
		reply, n := consumeQuery(q.buf, rows, cols, cursorRow, cursorCol)
		if n == 0 {
			if len(q.buf) > 128 {
				q.buf = q.buf[1:]
				continue
			}
			break
		}
		replies = append(replies, reply...)
		q.buf = q.buf[n:]
	}
	return replies
}

func consumeQuery(s []byte, rows, cols, cursorRow, cursorCol int) (reply []byte, consumed int) {
	if len(s) < 2 || s[0] != 0x1b {
		return nil, 0
	}
	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			if csiFinal(s[i]) {
				return replyCSI(s[:i+1], rows, cols, cursorRow, cursorCol), i + 1
			}
		}
		return nil, 0
	case ']':
		for i := 2; i < len(s); i++ {
			if oscTerminated(s[:i+1]) {
				return nil, i + 1
			}
		}
		return nil, 0
	case 'P':
		for i := 2; i < len(s); i++ {
			if oscTerminated(s[:i+1]) {
				return nil, i + 1
			}
		}
		return nil, 0
	default:
		return nil, 2
	}
}

func replyCSI(s []byte, rows, cols, cursorRow, cursorCol int) []byte {
	fin := s[len(s)-1]
	body := s[2 : len(s)-1]
	switch fin {
	case 'n':
		if bytes.Equal(body, []byte("6")) {
			return []byte(fmt.Sprintf("\x1b[%d;%dR", cprCell(cursorRow, rows), cprCell(cursorCol, cols)))
		}
		if bytes.Equal(body, []byte("5")) {
			return []byte("\x1b[0n")
		}
	case 'c':
		if len(body) == 0 || bytes.Equal(body, []byte("0")) {
			return []byte("\x1b[?1;2c")
		}
		if len(body) > 0 && body[0] == '>' {
			return []byte("\x1b[>0;1c")
		}
	case 'u':
		// Nvim probes with CSI ? u CSI c. Attach hides the query from the
		// emulator so kitty does not also reply. Without this answer nvim
		// still writes CSI > 3 u to the outer TTY, then skips the matching
		// pop on exit - the shell then sees CSI-u key reports and looks dead.
		if bytes.Equal(body, []byte("?")) {
			return []byte("\x1b[?0u")
		}
	}
	return nil
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
