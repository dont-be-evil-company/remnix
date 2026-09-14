package helpparse

import (
	"regexp"
	"strings"
	"unicode"
)

// Entity is one command or flag recovered from help text.
type Entity struct {
	Name  string
	Descr string
}

type sectionKind int

const (
	sectionOther sectionKind = iota
	sectionCommands
	sectionFlags
	sectionSkip
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

var flagToken = regexp.MustCompile(`^(?:-\w|--[\w-]+)$`)

var commandName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._+-]*$`)

var stopwords = map[string]struct{}{
	"is": {}, "one": {}, "of": {}, "the": {}, "following": {}, "and": {}, "or": {},
	"for": {}, "this": {}, "that": {}, "with": {}, "from": {}, "a": {}, "an": {},
	"to": {}, "in": {}, "on": {}, "as": {}, "by": {}, "at": {}, "be": {},
	"option": {}, "options": {}, "argument": {}, "arguments": {},
	"usage": {}, "synopsis": {}, "description": {}, "example": {},
	"examples": {}, "see": {}, "also": {},
}

func normalizeHelp(help string) string {
	help = ansiEscape.ReplaceAllString(help, "")
	help = stripBackspace(help)
	help = strings.ReplaceAll(help, "\r\n", "\n")
	help = strings.ReplaceAll(help, "\r", "\n")
	return help
}

// Parse extracts command and flag names plus descriptions from help.
// Unknown frameworks still run the generic column/indent engine.
func Parse(help string) []Entity {
	help = normalizeHelp(help)
	fw := Identify(help)
	return parseLines(strings.Split(help, "\n"), fw)
}

func parseLines(lines []string, fw Framework) []Entity {
	var ents []Entity
	seen := map[string]int{}
	section := sectionOther
	if fw == GoFlag {
		section = sectionFlags
	}
	pendingName := ""
	pendingIndent := -1
	pendingDescr := strings.Builder{}

	flushPending := func() {
		if pendingName == "" {
			return
		}
		addEntity(&ents, seen, pendingName, strings.TrimSpace(pendingDescr.String()))
		pendingName = ""
		pendingIndent = -1
		pendingDescr.Reset()
	}

	for i := 0; i < len(lines); i++ {
		raw := strings.TrimRight(lines[i], " \t")
		indent := leadingSpaces(raw)
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			if pendingName != "" && pendingDescr.Len() > 0 {
				flushPending()
			}
			continue
		}
		if kind, ok := classifyHeading(trimmed); ok {
			flushPending()
			section = kind
			continue
		}
		if isSynopsisLine(trimmed) {
			flushPending()
			continue
		}

		if pendingName != "" {
			if indent > pendingIndent && !looksLikeCommandRow(trimmed) && !looksLikeFlagRow(trimmed) && !isBulletCommand(trimmed) {
				if pendingDescr.Len() > 0 {
					pendingDescr.WriteByte(' ')
				}
				pendingDescr.WriteString(trimmed)
				continue
			}
			flushPending()
		}

		if section == sectionSkip {
			continue
		}

		if names, descr, ok := parseFlagRow(trimmed); ok && (section == sectionFlags || section == sectionOther || section == sectionCommands) {
			for _, n := range names {
				addEntity(&ents, seen, n, descr)
			}
			continue
		}

		if name, descr, ok := parseBulletRow(trimmed); ok {
			if descr != "" || section == sectionCommands || section == sectionOther {
				if descr == "" && i+1 < len(lines) {
					next := strings.TrimSpace(lines[i+1])
					nextIndent := leadingSpaces(strings.TrimRight(lines[i+1], " \t"))
					if next != "" && nextIndent > indent && !looksLikeCommandRow(next) && !isBulletCommand(next) {
						descr = next
						i++
					}
				}
				addEntity(&ents, seen, name, descr)
			}
			continue
		}

		if name, descr, ok := parseTwoColumnCommand(trimmed); ok {
			addEntity(&ents, seen, name, descr)
			continue
		}

		if name, ok := parseNameOnly(trimmed); ok && indent >= 1 {
			pendingName = name
			pendingIndent = indent
			pendingDescr.Reset()
			continue
		}
	}
	flushPending()
	return ents
}

func addEntity(ents *[]Entity, seen map[string]int, name, descr string) {
	name = strings.TrimSpace(name)
	descr = cleanDescr(descr)
	if name == "" {
		return
	}
	if i, ok := seen[name]; ok {
		if (*ents)[i].Descr == "" && descr != "" {
			(*ents)[i].Descr = descr
		}
		return
	}
	seen[name] = len(*ents)
	*ents = append(*ents, Entity{Name: name, Descr: descr})
}

func cleanDescr(d string) string {
	d = strings.TrimSpace(d)
	d = strings.TrimPrefix(d, "-")
	d = strings.TrimPrefix(d, "-")
	d = strings.TrimPrefix(d, "-")
	return strings.TrimSpace(d)
}

func leadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' || r == '\t' {
			n++
			continue
		}
		break
	}
	return n
}

func classifyHeading(line string) (sectionKind, bool) {
	t := strings.TrimSpace(line)
	t = strings.TrimRight(t, ":")
	if t == "" {
		return sectionOther, false
	}
	fields := strings.Fields(t)
	if len(fields) > 5 {
		return sectionOther, false
	}
	// Long sentences under a section ("GROUP is one of the following") are not headings.
	if len(fields) >= 3 && !isShouty(t) && !strings.HasSuffix(strings.TrimSpace(line), ":") {
		return sectionOther, false
	}
	if !strings.HasSuffix(strings.TrimSpace(line), ":") && !isShouty(t) && !looksLikeTitleHeading(t) {
		return sectionOther, false
	}
	key := strings.ToLower(t)
	switch {
	case strings.Contains(key, "alias"),
		strings.Contains(key, "see also"),
		strings.Contains(key, "example"),
		strings.Contains(key, "copyright"),
		strings.Contains(key, "author"),
		strings.Contains(key, "environment"),
		key == "name", key == "synopsis", key == "description", key == "see also":
		return sectionSkip, true
	case strings.Contains(key, "available command"),
		strings.Contains(key, "available service"),
		strings.Contains(key, "subcommand"),
		key == "commands", key == "command",
		key == "groups", key == "group",
		key == "services", key == "service":
		return sectionCommands, true
	case strings.Contains(key, "option"),
		strings.Contains(key, "flag"):
		return sectionFlags, true
	case isShouty(t) && (strings.Contains(key, "command") || strings.Contains(key, "group") || strings.Contains(key, "service")):
		return sectionCommands, true
	}
	if strings.HasSuffix(strings.TrimSpace(line), ":") && looksLikeTitleHeading(t) {
		return sectionOther, true
	}
	return sectionOther, false
}

func isShouty(s string) bool {
	letters := 0
	upper := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters++
			if unicode.IsUpper(r) {
				upper++
			}
		}
	}
	return letters > 0 && upper == letters
}

func looksLikeTitleHeading(s string) bool {
	for _, w := range strings.Fields(s) {
		if w == "" {
			continue
		}
		r := []rune(w)
		if unicode.IsLetter(r[0]) && !unicode.IsUpper(r[0]) {
			return false
		}
	}
	return len(strings.Fields(s)) <= 4
}

func isSynopsisLine(s string) bool {
	low := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(low, "usage:") || strings.HasPrefix(low, "usage ") ||
		strings.HasPrefix(low, "synopsis") || strings.HasPrefix(s, "Usage of ")
}

func looksLikeFlagRow(s string) bool {
	_, _, ok := parseFlagRow(s)
	return ok
}

func looksLikeCommandRow(s string) bool {
	_, _, ok := parseTwoColumnCommand(s)
	return ok
}

func parseFlagRow(s string) (names []string, descr string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" || (s[0] != '-') {
		return nil, "", false
	}
	// "-h, --help   desc" or "--foo FOO   desc" or "-v, --verbose"
	rest := s
	var toks []string
	for rest != "" {
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			break
		}
		if rest[0] != '-' {
			break
		}
		end := 1
		for end < len(rest) && rest[end] != ' ' && rest[end] != ',' && rest[end] != '\t' {
			end++
		}
		tok := rest[:end]
		if !flagToken.MatchString(tok) {
			return nil, "", false
		}
		toks = append(toks, tok)
		rest = rest[end:]
		rest = strings.TrimLeft(rest, " \t")
		if strings.HasPrefix(rest, ",") {
			rest = strings.TrimLeft(rest[1:], " \t")
			continue
		}
		break
	}
	if len(toks) == 0 {
		return nil, "", false
	}
	rest = strings.TrimSpace(rest)
	// Drop a metavar (FOO, <file>, string) before the description.
	if fields := strings.Fields(rest); len(fields) > 0 && !strings.HasPrefix(fields[0], "-") {
		if isMetavar(fields[0]) {
			rest = strings.TrimSpace(strings.TrimPrefix(rest, fields[0]))
			// two-space or tab gap expected before descr; if the rest is all metavar-like, descr may still follow
		}
	}
	descr = rest
	if gap := strings.Index(rest, "  "); gap >= 0 {
		head, tail := strings.TrimSpace(rest[:gap]), strings.TrimSpace(rest[gap:])
		if isMetavar(head) || head == "" {
			descr = tail
		} else if looksLikeDescrStart(tail) {
			descr = tail
		}
	}
	return toks, cleanDescr(descr), true
}

func isMetavar(s string) bool {
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "<") || strings.HasPrefix(s, "[") {
		return true
	}
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.IsUpper(r) && r != '_' {
			return false
		}
	}
	return true
}

func looksLikeDescrStart(s string) bool {
	if s == "" {
		return false
	}
	r := []rune(s)[0]
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func parseTwoColumnCommand(s string) (name, descr string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" || s[0] == '-' || s[0] == '{' {
		return "", "", false
	}
	// Two or more spaces (or a tab) between name and description.
	gap := -1
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '\t' || (s[i] == ' ' && s[i+1] == ' ') {
			gap = i
			break
		}
	}
	if gap < 0 {
		return "", "", false
	}
	name = strings.TrimSpace(s[:gap])
	descr = strings.TrimSpace(s[gap:])
	if !validCommandName(name) || descr == "" {
		return "", "", false
	}
	return name, cleanDescr(descr), true
}

func parseBulletRow(s string) (name, descr string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	switch s[0] {
	case 'o', '+', '*':
		if len(s) < 3 || (s[1] != ' ' && s[1] != '\t') {
			return "", "", false
		}
		rest := strings.TrimSpace(s[1:])
		return parseTwoColumnOrName(rest)
	}
	return "", "", false
}

func isBulletCommand(s string) bool {
	_, _, ok := parseBulletRow(s)
	return ok
}

func parseTwoColumnOrName(s string) (name, descr string, ok bool) {
	fields := strings.Fields(s)
	if len(fields) == 0 || !validCommandName(fields[0]) {
		return "", "", false
	}
	if name, descr, ok := parseTwoColumnCommand(s); ok {
		return name, descr, true
	}
	if len(fields) == 1 {
		return fields[0], "", true
	}
	return "", "", false
}

func parseNameOnly(s string) (string, bool) {
	fields := strings.Fields(s)
	if len(fields) != 1 {
		return "", false
	}
	name := strings.TrimRight(fields[0], ":")
	if !validCommandName(name) {
		return "", false
	}
	return name, true
}

func validCommandName(name string) bool {
	if name == "" || strings.ContainsAny(name, "[]{}<>()|") {
		return false
	}
	if _, skip := stopwords[strings.ToLower(name)]; skip {
		return false
	}
	if strings.EqualFold(name, "COMMAND") || strings.EqualFold(name, "GROUP") {
		return false
	}
	return commandName.MatchString(name)
}

func stripBackspace(s string) string {
	if !strings.ContainsRune(s, '\b') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\b' {
			out := []rune(b.String())
			if len(out) == 0 {
				continue
			}
			b.Reset()
			b.WriteString(string(out[:len(out)-1]))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
