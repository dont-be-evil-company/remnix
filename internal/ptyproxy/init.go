package ptyproxy

import (
	"path/filepath"
	"strings"
)

// Preamble is the exec-wrapper sourced at the top of shell rc. After exec,
// the same rc is sourced again; EnvActive makes the second pass a no-op.
func Preamble(bin, shell string) string {
	if bin == "" {
		bin = "remnix"
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
		return "remnix-attach"
	}
	return filepath.Join(dir, "remnix-attach")
}

func posixPreamble(bin, attach string) string {
	qb := posixQuote(bin)
	qa := posixQuote(attach)
	return `if [[ "$-" == *i* ]] && [[ -t 0 ]] && [[ -t 1 ]]; then
  _remnix_pty_tmux_current="${TMUX:-}"
  _remnix_pty_tmux_previous="${REMNIX_PTY_PROXY_TMUX:-}"
  if [[ -z "${REMNIX_PTY_PROXY_ACTIVE:-}${REMNIX_SESSION_ACTIVE:-}" ]] || [[ "$_remnix_pty_tmux_current" != "$_remnix_pty_tmux_previous" ]]; then
    export REMNIX_PTY_PROXY_ACTIVE=1
    export REMNIX_SESSION_ACTIVE=1
    export REMNIX_PTY_PROXY_TMUX="$_remnix_pty_tmux_current"
    _remnix_pty_zsh=""
    if [[ -n "${ZSH_VERSION:-}" ]]; then
      _remnix_pty_zsh="${ZSH_ARGZERO:-$(command -v zsh)}"
      _remnix_pty_zsh="${_remnix_pty_zsh#-}"
    fi
    # A failed exec in zsh exits the session. Never leave the user without a shell.
    if [[ -z "${REMNIX_PTY_PROXY_LEGACY:-}" ]] && [[ -x ` + qa + ` ]]; then
      if [[ -n "${BASH_VERSION:-}" ]]; then
        exec ` + qa + ` --shell "$BASH" --remnix ` + qb + ` || true
      elif [[ -n "${ZSH_VERSION:-}" ]]; then
        exec ` + qa + ` --shell "$_remnix_pty_zsh" --remnix ` + qb + ` || true
      else
        exec ` + qa + ` --remnix ` + qb + ` || true
      fi
    fi
    if [[ -n "${BASH_VERSION:-}" ]]; then
      exec ` + qb + ` pty-proxy --shell "$BASH" || true
    elif [[ -n "${ZSH_VERSION:-}" ]]; then
      exec ` + qb + ` pty-proxy --shell "$_remnix_pty_zsh" || true
    else
      exec ` + qb + ` pty-proxy || true
    fi
  fi
  unset _remnix_pty_tmux_current _remnix_pty_tmux_previous _remnix_pty_zsh
fi
`
}

func fishPreamble(bin, attach string) string {
	qb := fishQuote(bin)
	qa := fishQuote(attach)
	return `if status is-interactive; and test -c /dev/tty
  set -l _remnix_pty_tmux_current ""
  if set -q TMUX
    set _remnix_pty_tmux_current "$TMUX"
  end
  set -l _remnix_pty_tmux_previous ""
  if set -q REMNIX_PTY_PROXY_TMUX
    set _remnix_pty_tmux_previous "$REMNIX_PTY_PROXY_TMUX"
  end
  # exec fish must not nest another attach on this PTY. Only wrap again
  # when TMUX becomes a new non-empty value (a new multiplexer pane).
  set -l _remnix_need_wrap 0
  if not set -q REMNIX_PTY_PROXY_ACTIVE; and not set -q REMNIX_SESSION_ACTIVE
    set _remnix_need_wrap 1
  else if test -n "$_remnix_pty_tmux_current"; and test "$_remnix_pty_tmux_current" != "$_remnix_pty_tmux_previous"
    set _remnix_need_wrap 1
  end
  if test "$_remnix_need_wrap" = 1
    set -gx REMNIX_PTY_PROXY_ACTIVE 1
    set -gx REMNIX_SESSION_ACTIVE 1
    set -gx REMNIX_PTY_PROXY_TMUX "$_remnix_pty_tmux_current"
    if not set -q REMNIX_PTY_PROXY_LEGACY; and test -x ` + qa + `
      exec ` + qa + ` --shell (status fish-path) --remnix ` + qb + ` </dev/tty; or true
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
  let tmux_previous = ($env.REMNIX_PTY_PROXY_TMUX? | default "")
  if (($env.REMNIX_PTY_PROXY_ACTIVE? | default "") | is-empty) or ($tmux_current != $tmux_previous) {
    $env.REMNIX_PTY_PROXY_ACTIVE = "1"
    $env.REMNIX_SESSION_ACTIVE = "1"
    $env.REMNIX_PTY_PROXY_TMUX = $tmux_current
    if ($env.REMNIX_PTY_PROXY_LEGACY? | default "") == "" and (` + qa + ` | path exists) {
      try { exec ` + qa + ` --shell $nu.current-exe --remnix ` + qb + ` } catch { }
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
