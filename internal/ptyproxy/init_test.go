package ptyproxy

import (
	"strings"
	"testing"
)

func TestPreambleExecAndGuards(t *testing.T) {
	zsh := Preamble("/opt/remnix", "zsh")
	for _, want := range []string{
		"REMNIX_PTY_PROXY_ACTIVE",
		"REMNIX_PTY_PROXY_TMUX",
		`exec '/opt/remnix-attach' --shell "$BASH" --remnix '/opt/remnix' || true`,
		`exec '/opt/remnix-attach' --shell "$_remnix_pty_zsh" --remnix '/opt/remnix' || true`,
		"ZSH_ARGZERO",
		"[[ -x '/opt/remnix-attach' ]]",
		`[[ "$-" == *i* ]]`,
		"|| true",
	} {
		if !strings.Contains(zsh, want) {
			t.Fatalf("zsh preamble missing %q\n%s", want, zsh)
		}
	}
	fish := Preamble("/opt/remnix", "fish")
	for _, want := range []string{
		"exec '/opt/remnix-attach' --shell (status fish-path) --remnix '/opt/remnix' </dev/tty; or true",
		"status is-interactive",
		"test -c /dev/tty",
		"REMNIX_PTY_PROXY_ACTIVE",
		"test -x '/opt/remnix-attach'",
		"_remnix_need_wrap",
		`test -n "$_remnix_pty_tmux_current"`,
	} {
		if !strings.Contains(fish, want) {
			t.Fatalf("fish preamble missing %q\n%s", want, fish)
		}
	}
	if strings.Contains(fish, "test -t 0") {
		t.Fatal("fish preamble must not require a TTY stdin; `| source` makes stdin a pipe")
	}
	nu := Preamble("/opt/remnix", "nu")
	for _, want := range []string{
		`exec "/opt/remnix-attach" --shell $nu.current-exe --remnix "/opt/remnix"`,
		"$nu.is-interactive",
		`"/dev/tty" | path exists`,
		"REMNIX_PTY_PROXY_ACTIVE",
	} {
		if !strings.Contains(nu, want) {
			t.Fatalf("nu preamble missing %q\n%s", want, nu)
		}
	}
	if strings.Contains(nu, "is-terminal") {
		t.Fatal("nu preamble must not require is-terminal; config.nu load makes it false")
	}
}
