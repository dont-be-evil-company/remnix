package helpparse

import "strings"

// Framework is the CLI library that generated a --help blob.
type Framework string

const (
	Unknown  Framework = ""
	Argparse Framework = "argparse"
	Click    Framework = "click"
	Cobra    Framework = "cobra"
	GnuArgp  Framework = "gnu-argp"
	Busybox  Framework = "busybox"
	GoFlag   Framework = "go-flag"
	BsdTerse Framework = "bsd-terse"
)

// Literal substring → framework. First match wins. Ported from mandible-extract
// help_text_signature.rs: more specific markers before coarse fallbacks.
var signatures = []struct {
	marker     string
	framework  Framework
	ignoreCase bool
}{
	{marker: "show this help message and exit", framework: Argparse, ignoreCase: true},
	{marker: "Show this message and exit.", framework: Click},
	{marker: "Available Commands:", framework: Cobra},
	{marker: "Mandatory arguments to long options are mandatory for short options too.", framework: GnuArgp},
	{marker: "Mandatory or optional arguments to long options are also mandatory or optional", framework: GnuArgp},
	{marker: "BusyBox is copyrighted", framework: Busybox},
}

// Identify reports which framework produced help, or Unknown.
func Identify(help string) Framework {
	lower := strings.ToLower(help)
	for _, sig := range signatures {
		if sig.ignoreCase {
			if strings.Contains(lower, strings.ToLower(sig.marker)) {
				return sig.framework
			}
			continue
		}
		if strings.Contains(help, sig.marker) {
			return sig.framework
		}
	}
	if scanGoFlagUsage(help) {
		return GoFlag
	}
	if looksLikeBSDTerse(help) {
		return BsdTerse
	}
	return Unknown
}

func scanGoFlagUsage(help string) bool {
	for _, line := range strings.Split(help, "\n") {
		if strings.HasPrefix(line, "Usage of ") && strings.HasSuffix(strings.TrimRight(line, "\r"), ":") {
			return true
		}
	}
	return false
}

func looksLikeBSDTerse(help string) bool {
	hasUsage := false
	nonBlank := 0
	for _, line := range strings.Split(help, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		nonBlank++
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "usage:") {
			hasUsage = true
		}
	}
	return hasUsage && !strings.Contains(help, "--") && nonBlank > 0 && nonBlank <= 20
}
