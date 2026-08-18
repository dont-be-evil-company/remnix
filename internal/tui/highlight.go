package tui

import (
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
)

var (
	colCommand  = lipgloss.Color("#89B4FA")
	colKeyword  = lipgloss.Color("#CBA6F7")
	colFlag     = lipgloss.Color("#FAB387")
	colString   = lipgloss.Color("#A6E3A1")
	colComment  = lipgloss.Color("#6C7086")
	colOperator = lipgloss.Color("#F38BA8")
	colVar      = lipgloss.Color("#89DCEB")
	colPath     = lipgloss.Color("#94E2D5")
	colNumber   = lipgloss.Color("#F9E2AF")
	colArg      = lipgloss.Color("#CDD6F4")
)

var styleCommand = lipgloss.NewStyle().Foreground(colCommand).Bold(true)
var styleKeyword = lipgloss.NewStyle().Foreground(colKeyword).Bold(true)
var styleFlag = lipgloss.NewStyle().Foreground(colFlag)
var styleString = lipgloss.NewStyle().Foreground(colString)
var styleComment = lipgloss.NewStyle().Foreground(colComment).Italic(true)
var styleOperator = lipgloss.NewStyle().Foreground(colOperator)
var styleVar = lipgloss.NewStyle().Foreground(colVar)
var stylePath = lipgloss.NewStyle().Foreground(colPath)
var styleNumber = lipgloss.NewStyle().Foreground(colNumber)
var styleArg = lipgloss.NewStyle().Foreground(colArg)

var shellKeywords = map[string]struct{}{
	"if": {}, "then": {}, "else": {}, "elif": {}, "fi": {},
	"for": {}, "while": {}, "until": {}, "do": {}, "done": {},
	"case": {}, "esac": {}, "in": {}, "select": {},
	"function": {}, "time": {}, "coproc": {}, "return": {},
	"exit": {}, "break": {}, "continue": {}, "shift": {},
	"local": {}, "export": {}, "declare": {}, "typeset": {},
	"readonly": {}, "unset": {}, "eval": {}, "exec": {},
	"source": {}, "alias": {}, "unalias": {}, "set": {},
	"true": {}, "false": {}, "test": {},
}

func HighlightCommand(cmd string) string {
	if cmd == "" {
		return ""
	}
	runes := []rune(cmd)
	var b strings.Builder
	b.Grow(len(cmd) * 2)
	i := 0
	cmdPos := true
	for i < len(runes) {
		r := runes[i]
		switch {
		case r == '#' && (i == 0 || unicode.IsSpace(runes[i-1])):
			b.WriteString(styleComment.Render(string(runes[i:])))
			return b.String()
		case r == '\'' || r == '"':
			frag, next := readQuoted(runes, i)
			b.WriteString(styleString.Render(frag))
			i = next
		case r == '$':
			frag, next := readDollar(runes, i)
			b.WriteString(styleVar.Render(frag))
			i = next
			cmdPos = false
		case isOperatorStart(r):
			frag, next, reset := readOperator(runes, i)
			b.WriteString(styleOperator.Render(frag))
			i = next
			if reset {
				cmdPos = true
			}
		case unicode.IsSpace(r):
			b.WriteRune(r)
			i++
		default:
			word, next := readWord(runes, i)
			b.WriteString(styleWord(word, cmdPos).Render(word))
			if cmdPos && !isAssignment(word) {
				_, kw := shellKeywords[word]
				if !kw {
					cmdPos = false
				}
			}
			i = next
		}
	}
	return b.String()
}

func styleWord(word string, cmdPos bool) lipgloss.Style {
	if cmdPos {
		if _, ok := shellKeywords[word]; ok {
			return styleKeyword
		}
		if isAssignment(word) {
			return styleVar
		}
		return styleCommand
	}
	if strings.HasPrefix(word, "-") && len(word) > 1 {
		return styleFlag
	}
	if isNumber(word) {
		return styleNumber
	}
	if strings.Contains(word, "/") || strings.HasPrefix(word, "./") || strings.HasPrefix(word, "~") {
		return stylePath
	}
	return styleArg
}

func isAssignment(word string) bool {
	eq := strings.IndexByte(word, '=')
	if eq <= 0 {
		return false
	}
	for _, r := range word[:eq] {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			return false
		}
	}
	return true
}

func isNumber(word string) bool {
	if word == "" {
		return false
	}
	dots := 0
	for _, r := range word {
		if r == '.' {
			dots++
			if dots > 1 {
				return false
			}
			continue
		}
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isOperatorStart(r rune) bool {
	return r == '|' || r == '&' || r == ';' || r == '`' || r == '(' || r == ')' || r == '{' || r == '}' || r == '<' || r == '>' || r == '!'
}

func readQuoted(runes []rune, i int) (string, int) {
	q := runes[i]
	j := i + 1
	for j < len(runes) {
		if runes[j] == '\\' && q == '"' && j+1 < len(runes) {
			j += 2
			continue
		}
		if runes[j] == q {
			j++
			break
		}
		j++
	}
	return string(runes[i:j]), j
}

func readDollar(runes []rune, i int) (string, int) {
	if i+1 >= len(runes) {
		return "$", i + 1
	}
	switch runes[i+1] {
	case '{':
		j := i + 2
		for j < len(runes) && runes[j] != '}' {
			j++
		}
		if j < len(runes) {
			j++
		}
		return string(runes[i:j]), j
	case '(':
		j := i + 2
		depth := 1
		for j < len(runes) && depth > 0 {
			switch runes[j] {
			case '(':
				depth++
			case ')':
				depth--
			}
			j++
		}
		return string(runes[i:j]), j
	default:
		j := i + 1
		if runes[j] == '#' || runes[j] == '?' || runes[j] == '$' || runes[j] == '!' || runes[j] == '*' || runes[j] == '@' {
			return string(runes[i : j+1]), j + 1
		}
		for j < len(runes) && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j]) || runes[j] == '_') {
			j++
		}
		if j == i+1 {
			return "$", i + 1
		}
		return string(runes[i:j]), j
	}
}

func readOperator(runes []rune, i int) (string, int, bool) {
	r := runes[i]
	reset := r == '|' || r == '&' || r == ';' || r == '`' || r == '(' || r == '{'
	if i+1 < len(runes) {
		n := runes[i+1]
		if (r == '&' && n == '&') || (r == '|' && n == '|') || (r == '>' && (n == '>' || n == '&')) || (r == '<' && (n == '<' || n == '&')) {
			if r == '&' || r == '|' {
				reset = true
			}
			return string(runes[i : i+2]), i + 2, reset
		}
	}
	if r == '>' || r == '<' {
		reset = false
	}
	return string(r), i + 1, reset
}

func readWord(runes []rune, i int) (string, int) {
	j := i
	for j < len(runes) {
		r := runes[j]
		if unicode.IsSpace(r) || isOperatorStart(r) || r == '\'' || r == '"' || r == '$' || r == '#' {
			break
		}
		j++
	}
	return string(runes[i:j]), j
}
