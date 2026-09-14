package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/dont-be-evil-company/remnix/internal/config"
)

type Theme struct {
	Colors   config.Colors
	Icons    config.Icons
	Title    lipgloss.Style
	Muted    lipgloss.Style
	Accent   lipgloss.Style
	Duration lipgloss.Style
	Failed   lipgloss.Style
	Time     lipgloss.Style
	Badge    lipgloss.Style
	HelpKey  lipgloss.Style
	Help     lipgloss.Style
	Rule     lipgloss.Style
	Select   lipgloss.Style
	Command  lipgloss.Style
	Keyword  lipgloss.Style
	Flag     lipgloss.Style
	String   lipgloss.Style
	Comment  lipgloss.Style
	Operator lipgloss.Style
	Var      lipgloss.Style
	Path     lipgloss.Style
	Number   lipgloss.Style
	Arg      lipgloss.Style
	Text     lipgloss.Style
	ready    bool
}

func NewTheme(ui config.UI) Theme {
	fg := func(c string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(c))
	}
	th := Theme{Colors: ui.Colors, Icons: ui.Icons, ready: true}
	th.Title = fg(ui.Colors.TitleOrDefault()).Bold(true)
	th.Muted = fg(ui.Colors.MutedOrDefault())
	th.Accent = fg(ui.Colors.AccentOrDefault()).Bold(true)
	th.Duration = fg(ui.Colors.DurationOrDefault())
	th.Failed = fg(ui.Colors.FailedOrDefault())
	th.Time = fg(ui.Colors.TimeOrDefault())
	th.Badge = fg(ui.Colors.BadgeOrDefault()).Bold(true)
	th.HelpKey = fg(ui.Colors.AccentOrDefault())
	th.Help = fg(ui.Colors.MutedOrDefault())
	th.Rule = fg(ui.Colors.RuleOrDefault())
	th.Select = lipgloss.NewStyle().Background(lipgloss.Color(ui.Colors.SelectOrDefault()))
	th.Text = fg(ui.Colors.TextOrDefault())
	syn := ui.Colors.Syntax
	th.Command = fg(colorOrSyntax(syn.Command, "#89B4FA")).Bold(true)
	th.Keyword = fg(colorOrSyntax(syn.Keyword, "#CBA6F7")).Bold(true)
	th.Flag = fg(colorOrSyntax(syn.Flag, "#FAB387"))
	th.String = fg(colorOrSyntax(syn.String, "#A6E3A1"))
	th.Comment = fg(colorOrSyntax(syn.Comment, "#6C7086")).Italic(true)
	th.Operator = fg(colorOrSyntax(syn.Operator, "#F38BA8"))
	th.Var = fg(colorOrSyntax(syn.Variable, "#89DCEB"))
	th.Path = fg(colorOrSyntax(syn.Path, "#94E2D5"))
	th.Number = fg(colorOrSyntax(syn.Number, "#F9E2AF"))
	th.Arg = fg(colorOrSyntax(syn.Argument, "#CDD6F4"))
	return th
}

func (t Theme) OrDefault() Theme {
	if !t.ready {
		return DefaultTheme()
	}
	return t
}

func colorOrSyntax(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func DefaultTheme() Theme {
	return NewTheme(config.UI{})
}

func (t Theme) Cursor() string { return t.Icons.CursorOrDefault() }

// ForSelect returns a copy whose styles include the select-row background so
// syntax colors and other fragments keep that fill instead of resetting it.
func (t Theme) ForSelect() Theme {
	t = t.OrDefault()
	bg := lipgloss.Color(t.Colors.SelectOrDefault())
	paint := func(s lipgloss.Style) lipgloss.Style {
		return s.Background(bg)
	}
	t.Title = paint(t.Title)
	t.Muted = paint(t.Muted)
	t.Accent = paint(t.Accent)
	t.Duration = paint(t.Duration)
	t.Failed = paint(t.Failed)
	t.Time = paint(t.Time)
	t.Badge = paint(t.Badge)
	t.HelpKey = paint(t.HelpKey)
	t.Help = paint(t.Help)
	t.Rule = paint(t.Rule)
	t.Command = paint(t.Command)
	t.Keyword = paint(t.Keyword)
	t.Flag = paint(t.Flag)
	t.String = paint(t.String)
	t.Comment = paint(t.Comment)
	t.Operator = paint(t.Operator)
	t.Var = paint(t.Var)
	t.Path = paint(t.Path)
	t.Number = paint(t.Number)
	t.Arg = paint(t.Arg)
	t.Text = paint(t.Text)
	return t
}

const ansiReset = "\x1b[0m"

// PaintSelect puts the select background behind s, including already-styled
// fragments, and pads to width. It does not wrap with lipgloss Width, which
// leaves inner SGR resets on the terminal default background.
func (t Theme) PaintSelect(s string, width int) string {
	hex := t.Colors.SelectOrDefault()
	if !t.ready {
		hex = DefaultTheme().Colors.SelectOrDefault()
	}
	r, g, b, ok := hexRGB(hex)
	if !ok {
		return s
	}
	bg := fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
	s = strings.ReplaceAll(s, "\x1b[m", ansiReset)
	s = strings.ReplaceAll(s, ansiReset, ansiReset+bg)
	s = bg + s
	if pad := width - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s + ansiReset
}

func hexRGB(s string) (r, g, b int, ok bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	switch len(s) {
	case 3:
		rn, err1 := strconv.ParseUint(s[0:1], 16, 8)
		gn, err2 := strconv.ParseUint(s[1:2], 16, 8)
		bn, err3 := strconv.ParseUint(s[2:3], 16, 8)
		if err1 != nil || err2 != nil || err3 != nil {
			return 0, 0, 0, false
		}
		return int(rn * 17), int(gn * 17), int(bn * 17), true
	case 6:
		rn, err1 := strconv.ParseUint(s[0:2], 16, 8)
		gn, err2 := strconv.ParseUint(s[2:4], 16, 8)
		bn, err3 := strconv.ParseUint(s[4:6], 16, 8)
		if err1 != nil || err2 != nil || err3 != nil {
			return 0, 0, 0, false
		}
		return int(rn), int(gn), int(bn), true
	default:
		return 0, 0, 0, false
	}
}

func (t Theme) Separator() string { return t.Icons.SeparatorOrDefault() }

func (t Theme) Move() string { return t.Icons.MoveUpDownOrDefault() }
