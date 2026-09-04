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
		"exec '/opt/syncsh' pty-proxy --shell (status fish-path) </dev/tty",
		"status is-interactive",
		"test -c /dev/tty",
		"SYNCSH_PTY_PROXY_ACTIVE",
	} {
		if !strings.Contains(fish, want) {
			t.Fatalf("fish preamble missing %q\n%s", want, fish)
		}
	}
	if strings.Contains(fish, "test -t 0") {
		t.Fatal("fish preamble must not require a TTY stdin; `| source` makes stdin a pipe")
	}
	nu := Preamble("/opt/syncsh", "nu")
	for _, want := range []string{
		`exec "/opt/syncsh" pty-proxy --shell $nu.current-exe`,
		"$nu.is-interactive",
		`"/dev/tty" | path exists`,
		"SYNCSH_PTY_PROXY_ACTIVE",
	} {
		if !strings.Contains(nu, want) {
			t.Fatalf("nu preamble missing %q\n%s", want, nu)
		}
	}
	if strings.Contains(nu, "is-terminal") {
		t.Fatal("nu preamble must not require is-terminal; config.nu load makes it false")
	}
}
