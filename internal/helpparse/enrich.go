package helpparse

import (
	"strings"
)

// HelpArgv returns the complete command words to probe for help.
// A trailing partial token (no trailing space, more than one word) is dropped.
func HelpArgv(prefix string) []string {
	trimmed := strings.TrimSpace(prefix)
	if trimmed == "" {
		return nil
	}
	words := strings.Fields(trimmed)
	if !strings.HasSuffix(prefix, " ") && len(words) > 1 {
		words = words[:len(words)-1]
	}
	for _, w := range words {
		if !safeArg(w) {
			return nil
		}
	}
	return words
}

// ItemArgv is the command path to probe for one overlay row. A full command
// line is used as-is; a bare token is appended to the prefix command.
func ItemArgv(prefix, item string) []string {
	tok := matchToken(item)
	if tok == "" || strings.HasPrefix(tok, "-") || !safeArg(tok) {
		return nil
	}
	var words []string
	for _, w := range strings.Fields(item) {
		if strings.HasPrefix(w, "-") {
			break
		}
		if !safeArg(w) {
			return nil
		}
		words = append(words, w)
	}
	if len(words) > 1 {
		return words
	}
	parent := HelpArgv(prefix)
	if len(parent) == 0 {
		if len(words) == 1 {
			return words
		}
		return nil
	}
	if len(words) == 1 && argvEqual(words, parent) {
		return nil
	}
	if parent[len(parent)-1] == tok {
		return nil
	}
	return append(append([]string{}, parent...), tok)
}

func argvEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// SafeArg reports whether w is safe to pass to a help probe.
func SafeArg(w string) bool {
	return safeArg(w)
}

func safeArg(w string) bool {
	if w == "" || w == "-" || w == "--" {
		return false
	}
	for _, r := range w {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == '/' || r == '+' || r == '=' || r == ':' || r == '@' || r == '~':
		default:
			return false
		}
	}
	return true
}

// NeedsEnrich reports whether any item is missing a description.
func NeedsEnrich(items, descrs []string) bool {
	if len(items) == 0 {
		return false
	}
	for i := range items {
		if i >= len(descrs) || strings.TrimSpace(descrs[i]) == "" {
			return true
		}
	}
	return false
}

// Enrich fills empty descriptions by matching each item's last token to an entity.
// Non-empty compsys descriptions are left unchanged.
func Enrich(prefix string, items, descrs []string, entities []Entity) []string {
	out := make([]string, len(items))
	byName := map[string]string{}
	byLower := map[string]string{}
	for _, e := range entities {
		if e.Descr == "" {
			continue
		}
		byName[e.Name] = e.Descr
		low := strings.ToLower(e.Name)
		if _, ok := byLower[low]; !ok {
			byLower[low] = e.Descr
		}
	}
	for i, item := range items {
		if i < len(descrs) {
			out[i] = descrs[i]
		}
		if strings.TrimSpace(out[i]) != "" {
			continue
		}
		tok := matchToken(item)
		if tok == "" {
			continue
		}
		if d, ok := byName[tok]; ok {
			out[i] = d
			continue
		}
		if d, ok := byLower[strings.ToLower(tok)]; ok {
			out[i] = d
		}
	}
	return out
}

func matchToken(item string) string {
	fields := strings.Fields(item)
	if len(fields) == 0 {
		return ""
	}
	tok := fields[len(fields)-1]
	if i := strings.IndexByte(tok, '='); i > 0 && strings.HasPrefix(tok, "-") {
		tok = tok[:i]
	}
	return tok
}
