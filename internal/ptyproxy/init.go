package ptyproxy

import (
	"path/filepath"
	"strings"
)

// Preamble is the exec-wrapper sourced at the top of shell rc. After exec,
// the same rc is sourced again; EnvActive makes the second pass a no-op.
func Preamble(bin, shell string) string {
	if bin == "" {
		bin = "syncsh"
	}
	attach := attachBin(bin)
	switch strings.ToLower(shell) {
	case "zsh", "bash":
		return posixPreamble(bin, attach)
	case "fish":
		return fishPreamble(bin, attach)
	case "nu", "nushell":
		return nuPreamble(bin, attach)
	default:
		return posixPreamble(bin, attach)
	}
}

func attachBin(bin string) string {
	dir := filepath.Dir(bin)
	if dir == "." || dir == "" {
		return "syncsh-attach"
	}
	return filepath.Join(dir, "syncsh-attach")
}

func posixPreamble(bin, attach string) string {
	qb := posixQuote(bin)
	qa := posixQuote(attach)
	return `if [[ "$-" == *i* ]] && [[ -t 0 ]] && [[ -t 1 ]]; then
  _syncsh_pty_tmux_current="${TMUX:-}"
  _syncsh_pty_tmux_previous="${SYNCSH_PTY_PROXY_TMUX:-}"
  if [[ -z "${SYNCSH_PTY_PROXY_ACTIVE:-}${SYNCSH_SESSION_ACTIVE:-}" ]] || [[ "$_syncsh_pty_tmux_current" != "$_syncsh_pty_tmux_previous" ]]; then
    export SYNCSH_PTY_PROXY_ACTIVE=1
    export SYNCSH_SESSION_ACTIVE=1
    export SYNCSH_PTY_PROXY_TMUX="$_syncsh_pty_tmux_current"
    _syncsh_pty_zsh=""
    if [[ -n "${ZSH_VERSION:-}" ]]; then
      _syncsh_pty_zsh="${ZSH_ARGZERO:-$(command -v zsh)}"
      _syncsh_pty_zsh="${_syncsh_pty_zsh#-}"
    fi
    # A failed exec in zsh exits the session. Never leave the user without a shell.
    if [[ -z "${SYNCSH_PTY_PROXY_LEGACY:-}" ]] && [[ -x ` + qa + ` ]]; then
      if [[ -n "${BASH_VERSION:-}" ]]; then
        exec ` + qa + ` --shell "$BASH" --syncsh ` + qb + ` || true
      elif [[ -n "${ZSH_VERSION:-}" ]]; then
        exec ` + qa + ` --shell "$_syncsh_pty_zsh" --syncsh ` + qb + ` || true
      else
        exec ` + qa + ` --syncsh ` + qb + ` || true
      fi
    fi
    if [[ -n "${BASH_VERSION:-}" ]]; then
      exec ` + qb + ` pty-proxy --shell "$BASH" || true
    elif [[ -n "${ZSH_VERSION:-}" ]]; then
      exec ` + qb + ` pty-proxy --shell "$_syncsh_pty_zsh" || true
    else
      exec ` + qb + ` pty-proxy || true
    fi
  fi
  unset _syncsh_pty_tmux_current _syncsh_pty_tmux_previous _syncsh_pty_zsh
fi
`
}

func fishPreamble(bin, attach string) string {
	qb := fishQuote(bin)
	qa := fishQuote(attach)
	return `if status is-interactive; and test -c /dev/tty
  set -l _syncsh_pty_tmux_current ""
  if set -q TMUX
    set _syncsh_pty_tmux_current "$TMUX"
  end
  set -l _syncsh_pty_tmux_previous ""
  if set -q SYNCSH_PTY_PROXY_TMUX
    set _syncsh_pty_tmux_previous "$SYNCSH_PTY_PROXY_TMUX"
  end
  # exec fish must not nest another attach on this PTY. Only wrap again
  # when TMUX becomes a new non-empty value (a new multiplexer pane).
  set -l _syncsh_need_wrap 0
  if not set -q SYNCSH_PTY_PROXY_ACTIVE; and not set -q SYNCSH_SESSION_ACTIVE
    set _syncsh_need_wrap 1
  else if test -n "$_syncsh_pty_tmux_current"; and test "$_syncsh_pty_tmux_current" != "$_syncsh_pty_tmux_previous"
    set _syncsh_need_wrap 1
  end
  if test "$_syncsh_need_wrap" = 1
    set -gx SYNCSH_PTY_PROXY_ACTIVE 1
    set -gx SYNCSH_SESSION_ACTIVE 1
    set -gx SYNCSH_PTY_PROXY_TMUX "$_syncsh_pty_tmux_current"
    if not set -q SYNCSH_PTY_PROXY_LEGACY; and test -x ` + qa + `
      exec ` + qa + ` --shell (status fish-path) --syncsh ` + qb + ` </dev/tty; or true
    end
    exec ` + qb + ` pty-proxy --shell (status fish-path) </dev/tty; or true
  end
end
`
}

func nuPreamble(bin, attach string) string {
	qb := nuQuote(bin)
	qa := nuQuote(attach)
	return `if $nu.is-interactive and ("/dev/tty" | path exists) {
  let tmux_current = ($env.TMUX? | default "")
  let tmux_previous = ($env.SYNCSH_PTY_PROXY_TMUX? | default "")
  if (($env.SYNCSH_PTY_PROXY_ACTIVE? | default "") | is-empty) or ($tmux_current != $tmux_previous) {
    $env.SYNCSH_PTY_PROXY_ACTIVE = "1"
    $env.SYNCSH_SESSION_ACTIVE = "1"
    $env.SYNCSH_PTY_PROXY_TMUX = $tmux_current
    if ($env.SYNCSH_PTY_PROXY_LEGACY? | default "") == "" and (` + qa + ` | path exists) {
      try { exec ` + qa + ` --shell $nu.current-exe --syncsh ` + qb + ` } catch { }
    }
    try { exec ` + qb + ` pty-proxy --shell $nu.current-exe } catch { }
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
