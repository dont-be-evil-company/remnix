package helpparse

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const summaryMaxRunes = 160

// NodeSummary returns a one-line overlay blurb from a node's own help page.
// It prefers a NAME "cmd - summary" dash clause, then the first DESCRIPTION
// sentence. Empty NAME dashes (aws "s3 -") fall through to DESCRIPTION.
func NodeSummary(help string) string {
	help = normalizeHelp(help)
	if strings.TrimSpace(help) == "" {
		return ""
	}
	var (
		section   string
		nameParts []string
		descParts []string
	)
	for _, raw := range strings.Split(help, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			if section == "description" && len(descParts) > 0 {
				break
			}
			continue
		}
		if kind, ok := nodeSection(trimmed); ok {
			if section == "description" && len(descParts) > 0 && kind != "description" {
				break
			}
			section = kind
			continue
		}
		switch section {
		case "name":
			nameParts = append(nameParts, trimmed)
		case "description":
			descParts = append(descParts, trimmed)
		}
	}
	if s := nameDashSummary(strings.Join(nameParts, " ")); s != "" {
		return clipOneLine(s)
	}
	if len(descParts) == 0 {
		return ""
	}
	return clipOneLine(firstSentence(strings.Join(descParts, " ")))
}

func nodeSection(trimmed string) (string, bool) {
	key := strings.ToLower(strings.TrimRight(strings.TrimSpace(trimmed), ":"))
	switch key {
	case "name":
		return "name", true
	case "description", "overview":
		return "description", true
	}
	if _, ok := classifyHeading(trimmed); ok {
		return "stop", true
	}
	return "", false
}

func nameDashSummary(s string) string {
	s = collapseSpace(s)
	if s == "" {
		return ""
	}
	for _, sep := range []string{" - ", " - ", " - "} {
		if i := strings.Index(s, sep); i >= 0 {
			return strings.TrimSpace(s[i+len(sep):])
		}
	}
	return ""
}

func firstSentence(s string) string {
	s = collapseSpace(s)
	if s == "" {
		return ""
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '.' {
			continue
		}
		if i+1 == len(s) {
			return s
		}
		if s[i+1] != ' ' {
			continue
		}
		if isAbbrevDot(s, i) {
			continue
		}
		rest := strings.TrimSpace(s[i+1:])
		if rest == "" {
			return strings.TrimSpace(s[:i+1])
		}
		r, _ := utf8.DecodeRuneInString(rest)
		if unicode.IsUpper(r) {
			return strings.TrimSpace(s[:i+1])
		}
	}
	return s
}

func isAbbrevDot(s string, dot int) bool {
	if dot < 1 {
		return false
	}
	low := strings.ToLower(s)
	for _, a := range []string{"e.g.", "i.e.", "etc.", "vs."} {
		if dot+1 >= len(a) && low[dot+1-len(a):dot+1] == a {
			return true
		}
	}
	return false
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func clipOneLine(s string) string {
	s = collapseSpace(s)
	if s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= summaryMaxRunes {
		return s
	}
	runes := []rune(s)
	cut := summaryMaxRunes
	for cut > 0 && runes[cut-1] != ' ' {
		cut--
	}
	if cut < summaryMaxRunes/2 {
		cut = summaryMaxRunes
	}
	return strings.TrimSpace(string(runes[:cut]))
}
