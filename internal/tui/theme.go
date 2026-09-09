package tui

import (
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
}

func NewTheme(ui config.UI) Theme {
	fg := func(c string) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(c))
	}
	th := Theme{Colors: ui.Colors, Icons: ui.Icons}
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

func (t Theme) Separator() string { return t.Icons.SeparatorOrDefault() }

func (t Theme) Move() string { return t.Icons.MoveUpDownOrDefault() }
