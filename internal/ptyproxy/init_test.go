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
		`exec '/opt/syncsh-attach' --shell "$BASH" --syncsh '/opt/syncsh' || true`,
		`exec '/opt/syncsh-attach' --shell "$_syncsh_pty_zsh" --syncsh '/opt/syncsh' || true`,
		"ZSH_ARGZERO",
		"[[ -x '/opt/syncsh-attach' ]]",
		`[[ "$-" == *i* ]]`,
		"|| true",
	} {
		if !strings.Contains(zsh, want) {
			t.Fatalf("zsh preamble missing %q\n%s", want, zsh)
		}
	}
	fish := Preamble("/opt/syncsh", "fish")
	for _, want := range []string{
		"exec '/opt/syncsh-attach' --shell (status fish-path) --syncsh '/opt/syncsh' </dev/tty; or true",
		"status is-interactive",
		"test -c /dev/tty",
		"SYNCSH_PTY_PROXY_ACTIVE",
		"test -x '/opt/syncsh-attach'",
		"_syncsh_need_wrap",
		`test -n "$_syncsh_pty_tmux_current"`,
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
		`exec "/opt/syncsh-attach" --shell $nu.current-exe --syncsh "/opt/syncsh"`,
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
