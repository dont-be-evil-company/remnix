package terminal

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// kittyKeyDecoder turns CSI-u key reports into legacy bytes before they hit
// the inner PTY. After nvim (and other TUIs) the outer emulator can stay in
// kitty keyboard mode; zsh then sees \x1b[97u instead of 'a' and looks dead.
type kittyKeyDecoder struct {
	hold []byte
}

func (d *kittyKeyDecoder) feed(p []byte) []byte {
	if d == nil {
		return p
	}
	if len(d.hold) == 0 && len(p) == 0 {
		return p
	}
	buf := append(d.hold, p...)
	d.hold = d.hold[:0]
	out := make([]byte, 0, len(buf))
	i := 0
	for i < len(buf) {
		if buf[i] != 0x1b {
			out = append(out, buf[i])
			i++
			continue
		}
		if i+1 >= len(buf) {
			// Never hold a lone ESC across packets. Attach does the same;
			// otherwise Escape never reaches the shell or the overlay.
			out = append(out, 0x1b)
			break
		}
		if buf[i+1] != '[' {
			out = append(out, buf[i], buf[i+1])
			i += 2
			continue
		}
		j := i + 2
		for j < len(buf) && !csiFinal(buf[j]) {
			j++
		}
		if j >= len(buf) {
			if len(buf)-i > 64 {
				out = append(out, buf[i])
				i++
				continue
			}
			d.hold = append(d.hold[:0], buf[i:]...)
			break
		}
		seq := buf[i : j+1]
		fin := seq[len(seq)-1]
		if fin == 'I' || fin == 'O' {
			i = j + 1
			continue
		}
		if decoded, ok := decodeKittyKey(seq); ok {
			out = append(out, decoded...)
		} else {
			out = append(out, seq...)
		}
		i = j + 1
	}
	return out
}

func decodeKittyKey(seq []byte) ([]byte, bool) {
	n := len(seq)
	if n < 4 || seq[0] != 0x1b || seq[1] != '[' || seq[n-1] != 'u' {
		return nil, false
	}
	body := seq[2 : n-1]
	if len(body) == 0 {
		return nil, false
	}
	switch body[0] {
	case '?', '>', '<', '=':
		return nil, true
	}
	if body[0] < '0' || body[0] > '9' {
		return nil, false
	}
	fields := strings.Split(string(body), ";")
	keyPart := strings.Split(fields[0], ":")
	key, err := strconv.Atoi(keyPart[0])
	if err != nil || key < 0 {
		return nil, false
	}
	event := 1
	mods := 1
	if len(fields) > 1 && fields[1] != "" {
		mp := strings.Split(fields[1], ":")
		if mp[0] != "" {
			if m, err := strconv.Atoi(mp[0]); err == nil {
				mods = m
			}
		}
		if len(mp) > 1 && mp[1] != "" {
			if e, err := strconv.Atoi(mp[1]); err == nil {
				event = e
			}
		}
	}
	if event == 3 {
		return nil, true
	}
	if len(fields) > 2 && fields[2] != "" {
		var b []byte
		for _, cp := range strings.Split(fields[2], ":") {
			if cp == "" {
				continue
			}
			r, err := strconv.Atoi(cp)
			if err != nil || r <= 0 {
				continue
			}
			b = utf8.AppendRune(b, rune(r))
		}
		if len(b) > 0 {
			return b, true
		}
	}
	return kittyKeyToLegacy(key, mods), true
}

func kittyKeyToLegacy(key, mods int) []byte {
	if mods < 1 {
		mods = 1
	}
	bits := mods - 1
	ctrl := bits&4 != 0
	alt := bits&2 != 0
	shift := bits&1 != 0
	var b []byte
	switch key {
	case 27:
		b = []byte{0x1b}
	case 13:
		b = []byte{'\r'}
	case 9:
		b = []byte{'\t'}
	case 127:
		b = []byte{0x7f}
	default:
		if key >= 32 && key < 127 {
			ch := byte(key)
			if shift && ch >= 'a' && ch <= 'z' {
				ch -= 32
			}
			if ctrl {
				if ch >= 'a' && ch <= 'z' {
					ch -= 96
				} else if ch >= 'A' && ch <= 'Z' {
					ch -= 64
				} else if ch == ' ' || ch == '@' {
					ch = 0
				}
			}
			b = []byte{ch}
		} else if key > 127 && key < 0x110000 {
			b = utf8.AppendRune(nil, rune(key))
		} else {
			return nil
		}
	}
	if alt && len(b) > 0 && b[0] != 0x1b {
		return append([]byte{0x1b}, b...)
	}
	return b
}

// keyboardModeStripper removes kitty / modifyOtherKeys mode changes from PTY
// output so nvim cannot leave the outer emulator in CSI-u mode. Overlay paint
// still writes those sequences via sendFrame directly.
type keyboardModeStripper struct {
	hold []byte
}

func (s *keyboardModeStripper) feed(p []byte) []byte {
	if s == nil {
		return p
	}
	buf := append(s.hold, p...)
	s.hold = s.hold[:0]
	out := make([]byte, 0, len(buf))
	i := 0
	for i < len(buf) {
		if buf[i] != 0x1b {
			out = append(out, buf[i])
			i++
			continue
		}
		if i+1 >= len(buf) {
			s.hold = append(s.hold[:0], buf[i:]...)
			break
		}
		if buf[i+1] != '[' {
			out = append(out, buf[i], buf[i+1])
			i += 2
			continue
		}
		j := i + 2
		for j < len(buf) && !csiFinal(buf[j]) {
			j++
		}
		if j >= len(buf) {
			if len(buf)-i > 64 {
				out = append(out, buf[i])
				i++
				continue
			}
			s.hold = append(s.hold[:0], buf[i:]...)
			break
		}
		seq := buf[i : j+1]
		if keyboardModeCSI(seq) {
			i = j + 1
			continue
		}
		rewritten, drop, focusOff := rewriteFocusTracking(seq)
		if drop {
			if focusOff {
				out = append(out, "\x1b[?1004l"...)
			}
			i = j + 1
			continue
		}
		out = append(out, rewritten...)
		if focusOff {
			out = append(out, "\x1b[?1004l"...)
		}
		i = j + 1
	}
	return out
}

func keyboardModeCSI(seq []byte) bool {
	n := len(seq)
	if n < 4 || seq[0] != 0x1b || seq[1] != '[' {
		return false
	}
	fin := seq[n-1]
	body := seq[2 : n-1]
	if fin == 'u' && len(body) > 0 {
		switch body[0] {
		case '?', '>', '<', '=':
			return true
		}
		return false
	}
	if fin == 'm' && len(body) >= 2 && body[0] == '>' && body[1] == '4' {
		if len(body) == 2 || body[2] == ';' {
			return true
		}
	}
	return false
}

// rewriteFocusTracking removes DECSET/DECRST 1004 (focus reporting) so tmux's
// combined \x1b[?1;1004;2004h cannot arm Kitty to inject CSI I/O on pane focus.
func rewriteFocusTracking(seq []byte) (out []byte, drop, focusOff bool) {
	n := len(seq)
	if n < 5 || seq[0] != 0x1b || seq[1] != '[' || seq[2] != '?' {
		return seq, false, false
	}
	fin := seq[n-1]
	if fin != 'h' && fin != 'l' {
		return seq, false, false
	}
	parts := strings.Split(string(seq[3:n-1]), ";")
	kept := make([]string, 0, len(parts))
	hit := false
	for _, p := range parts {
		if p == "1004" {
			hit = true
			continue
		}
		if p != "" {
			kept = append(kept, p)
		}
	}
	if !hit {
		return seq, false, false
	}
	focusOff = fin == 'h'
	if len(kept) == 0 {
		return nil, true, focusOff
	}
	var b strings.Builder
	b.Grow(4 + 5*len(kept))
	b.WriteString("\x1b[?")
	b.WriteString(strings.Join(kept, ";"))
	b.WriteByte(fin)
	return []byte(b.String()), false, focusOff
}
