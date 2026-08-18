package config

import (
	"os"
	"strings"
)

// Expand substitutes $VAR / ${VAR} and ~ / ~/ in s.
func Expand(s string) string {
	if s == "" {
		return s
	}
	s = os.ExpandEnv(s)
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return s
	}
	if s == "~" {
		return home
	}
	if strings.HasPrefix(s, "~/") {
		return home + s[1:]
	}
	s = strings.ReplaceAll(s, " ~/", " "+home+"/")
	s = strings.ReplaceAll(s, "\t~/", "\t"+home+"/")
	if strings.HasSuffix(s, " ~") {
		return strings.TrimSuffix(s, " ~") + " " + home
	}
	return s
}
