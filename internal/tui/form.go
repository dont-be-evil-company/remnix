package tui

import (
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/dont-be-evil-company/remnix/internal/config"
)

// NewForm builds a huh form themed from the current remnix UI colors.
func NewForm(groups ...*huh.Group) *huh.Form {
	return huh.NewForm(groups...).WithTheme(formTheme().Huh())
}

func formTheme() Theme {
	cfg, err := config.Load()
	if err != nil {
		return DefaultTheme()
	}
	return NewTheme(cfg.UI)
}

// Huh maps remnix colors onto huh, including the select-row background.
func (t Theme) Huh() huh.Theme {
	t = t.OrDefault()
	accent := lipgloss.Color(t.Colors.AccentOrDefault())
	sel := lipgloss.Color(t.Colors.SelectOrDefault())
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		s := huh.ThemeCharm(isDark)
		s.Focused.SelectSelector = s.Focused.SelectSelector.Foreground(accent).Background(sel)
		s.Focused.SelectedOption = s.Focused.SelectedOption.Foreground(accent).Background(sel).Bold(true)
		s.Focused.MultiSelectSelector = s.Focused.MultiSelectSelector.Foreground(accent).Background(sel)
		s.Blurred.SelectSelector = s.Blurred.SelectSelector.Foreground(accent).Background(sel)
		s.Blurred.SelectedOption = s.Blurred.SelectedOption.Foreground(accent).Background(sel).Bold(true)
		s.Blurred.MultiSelectSelector = s.Blurred.MultiSelectSelector.Foreground(accent).Background(sel)
		return s
	})
}
