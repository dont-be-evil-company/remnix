package ptyproxy

import (
	"strings"
	"testing"
)

func TestPreambleExecAndGuards(t *testing.T) {
	zsh := Preamble("/opt/syncsh", "zsh")
	for _, want := range []string{
		"SYNCSH_PTY_PROXY_ACTIVE",
		"SYNCSH_PTY_PROXY_TMUX",
		`exec '/opt/syncsh' pty-proxy --shell "$BASH"`,
		`exec '/opt/syncsh' pty-proxy --shell "${_syncsh_pty_zsh#-}"`,
		"ZSH_ARGZERO",
		`[[ "$-" == *i* ]]`,
	} {
		if !strings.Contains(zsh, want) {
			t.Fatalf("zsh preamble missing %q\n%s", want, zsh)
		}
	}
	fish := Preamble("/opt/syncsh", "fish")
	for _, want := range []string{
		"exec '/opt/syncsh' pty-proxy --shell (status fish-path)",
		"status is-interactive",
		"SYNCSH_PTY_PROXY_ACTIVE",
	} {
		if !strings.Contains(fish, want) {
			t.Fatalf("fish preamble missing %q\n%s", want, fish)
		}
	}
	nu := Preamble("/opt/syncsh", "nu")
	for _, want := range []string{
		`exec "/opt/syncsh" pty-proxy --shell $nu.current-exe`,
		"is-terminal --stdin",
		"SYNCSH_PTY_PROXY_ACTIVE",
	} {
		if !strings.Contains(nu, want) {
			t.Fatalf("nu preamble missing %q\n%s", want, nu)
		}
	}
}
