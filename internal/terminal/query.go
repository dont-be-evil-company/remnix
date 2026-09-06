package terminal

import (
	"bytes"
	"fmt"
	"strings"
)

const (
	seqKittyFlagsOff = "\x1b[=0;1u"
	seqModifyKeysOff = "\x1b[>4;0m"
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

func (q *queryScanner) feed(p []byte, rows, cols int) []byte {
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
		reply, n := consumeQuery(q.buf, rows, cols)
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

func consumeQuery(s []byte, rows, cols int) (reply []byte, consumed int) {
	if len(s) < 2 || s[0] != 0x1b {
		return nil, 0
	}
	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			if csiFinal(s[i]) {
				return replyCSI(s[:i+1], rows, cols), i + 1
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

func replyCSI(s []byte, rows, cols int) []byte {
	fin := s[len(s)-1]
	body := s[2 : len(s)-1]
	switch fin {
	case 'n':
		if bytes.Equal(body, []byte("6")) {
			return []byte(fmt.Sprintf("\x1b[%d;%dR", rows, cols))
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

// withMainScreenKeyboardReset appends a keyboard-protocol disable when the
// chunk leaves the alternate screen (nvim, less, ...). Terminals that share one
// kitty stack between main and alt would otherwise keep CSI-u on after pop.
func withMainScreenKeyboardReset(p []byte) []byte {
	if !leavesAltScreen(p) {
		return p
	}
	return appendKeyboardReset(p)
}

func appendKeyboardReset(p []byte) []byte {
	out := make([]byte, 0, len(p)+len(seqKittyFlagsOff)+len(seqModifyKeysOff))
	out = append(out, p...)
	out = append(out, seqKittyFlagsOff...)
	out = append(out, seqModifyKeysOff...)
	return out
}

// altLeaveWatch catches rmcup split across PTY reads (CSI ? 1049 l).
type altLeaveWatch struct {
	tail []byte
}

func (w *altLeaveWatch) feed(p []byte) (out []byte, left bool) {
	const keep = 32
	check := p
	if len(w.tail) > 0 {
		check = append(append([]byte(nil), w.tail...), p...)
	}
	out = p
	if leavesAltScreen(check) && !leavesAltScreen(w.tail) {
		out = appendKeyboardReset(p)
		left = true
	}
	if n := len(p); n > keep {
		w.tail = append(w.tail[:0], p[n-keep:]...)
	} else {
		w.tail = append(w.tail[:0], p...)
	}
	return out, left
}

func leavesAltScreen(p []byte) bool {
	for i := 0; i < len(p); {
		j := bytes.Index(p[i:], []byte("\x1b["))
		if j < 0 {
			return false
		}
		i += j
		k := i + 2
		for k < len(p) && !csiFinal(p[k]) {
			k++
		}
		if k >= len(p) {
			return false
		}
		if p[i+2] == '?' && p[k] == 'l' {
			for _, part := range strings.Split(string(p[i+3:k]), ";") {
				if part == "1049" || part == "47" {
					return true
				}
			}
		}
		i = k + 1
	}
	return false
}
