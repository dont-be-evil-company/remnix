package ptyproxy

import "strings"

// Preamble is the exec-wrapper sourced at the top of shell rc. After exec,
// the same rc is sourced again; EnvActive makes the second pass a no-op.
func Preamble(bin, shell string) string {
	if bin == "" {
		bin = "syncsh"
	}
	switch strings.ToLower(shell) {
	case "zsh", "bash":
		return posixPreamble(bin)
	case "fish":
		return fishPreamble(bin)
	case "nu", "nushell":
		return nuPreamble(bin)
	default:
		return posixPreamble(bin)
	}
}

func posixPreamble(bin string) string {
	q := posixQuote(bin)
	return `if [[ "$-" == *i* ]] && [[ -t 0 ]] && [[ -t 1 ]]; then
  _syncsh_pty_tmux_current="${TMUX:-}"
  _syncsh_pty_tmux_previous="${SYNCSH_PTY_PROXY_TMUX:-}"
  if [[ -z "${SYNCSH_PTY_PROXY_ACTIVE:-}" ]] || [[ "$_syncsh_pty_tmux_current" != "$_syncsh_pty_tmux_previous" ]]; then
    export SYNCSH_PTY_PROXY_ACTIVE=1
    export SYNCSH_PTY_PROXY_TMUX="$_syncsh_pty_tmux_current"
    if [[ -n "${BASH_VERSION:-}" ]]; then
      exec ` + q + ` pty-proxy --shell "$BASH"
    elif [[ -n "${ZSH_VERSION:-}" ]]; then
      _syncsh_pty_zsh="${ZSH_ARGZERO:-$(command -v zsh)}"
      exec ` + q + ` pty-proxy --shell "${_syncsh_pty_zsh#-}"
    else
      exec ` + q + ` pty-proxy
    fi
  fi
  unset _syncsh_pty_tmux_current _syncsh_pty_tmux_previous
fi
`
}

func fishPreamble(bin string) string {
	q := fishQuote(bin)
	// `syncsh init fish | source` makes stdin a pipe, so test -t 0 is false
	// and exec would inherit that pipe. Reopen the real TTY on exec.
	return `if status is-interactive; and test -c /dev/tty
  set -l _syncsh_pty_tmux_current ""
  if set -q TMUX
    set _syncsh_pty_tmux_current "$TMUX"
  end
  set -l _syncsh_pty_tmux_previous ""
  if set -q SYNCSH_PTY_PROXY_TMUX
    set _syncsh_pty_tmux_previous "$SYNCSH_PTY_PROXY_TMUX"
  end
  if not set -q SYNCSH_PTY_PROXY_ACTIVE
    set -gx SYNCSH_PTY_PROXY_ACTIVE 1
    set -gx SYNCSH_PTY_PROXY_TMUX "$_syncsh_pty_tmux_current"
    exec ` + q + ` pty-proxy --shell (status fish-path) </dev/tty
  else if test "$_syncsh_pty_tmux_current" != "$_syncsh_pty_tmux_previous"
    set -gx SYNCSH_PTY_PROXY_ACTIVE 1
    set -gx SYNCSH_PTY_PROXY_TMUX "$_syncsh_pty_tmux_current"
    exec ` + q + ` pty-proxy --shell (status fish-path) </dev/tty
  end
end
`
}

func nuPreamble(bin string) string {
	q := nuQuote(bin)
	// is-terminal is false while config.nu loads (Nu captures config
	// output). exec anyway; pty-proxy reopens /dev/tty if fds are pipes.
	return `if $nu.is-interactive and ("/dev/tty" | path exists) {
  let tmux_current = ($env.TMUX? | default "")
  let tmux_previous = ($env.SYNCSH_PTY_PROXY_TMUX? | default "")
  if (($env.SYNCSH_PTY_PROXY_ACTIVE? | default "") | is-empty) or ($tmux_current != $tmux_previous) {
    $env.SYNCSH_PTY_PROXY_ACTIVE = "1"
    $env.SYNCSH_PTY_PROXY_TMUX = $tmux_current
    exec ` + q + ` pty-proxy --shell $nu.current-exe
  }
}
`
}

func posixQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func fishQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
}

func nuQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
