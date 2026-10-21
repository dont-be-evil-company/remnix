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
  _remnix_pty_tty="${TTY:-}"
  if [[ -z "$_remnix_pty_tty" ]]; then
    _remnix_pty_tty="$(command tty < /dev/tty 2>/dev/null)" || _remnix_pty_tty=""
  fi
  _remnix_pty_tty_previous="${REMNIX_PTY_PROXY_TTY:-}"
  _remnix_need_wrap=0
  if [[ -z "${REMNIX_PTY_PROXY_ACTIVE:-}${REMNIX_SESSION_ACTIVE:-}" ]]; then
    _remnix_need_wrap=1
  elif [[ "$_remnix_pty_tmux_current" != "$_remnix_pty_tmux_previous" ]]; then
    _remnix_need_wrap=1
  elif [[ -n "$_remnix_pty_tty_previous" && -n "$_remnix_pty_tty" && "$_remnix_pty_tty" != "$_remnix_pty_tty_previous" ]]; then
    # Nested PTY (nvim :terminal, emacs term, screen). TMUX is not the only
    # extra slave; without this the inner shell keeps the outer session id
    # and Ctrl+R places the overlay on nvim's alt-screen.
    _remnix_need_wrap=1
  fi
  if [[ "$_remnix_need_wrap" == 1 ]]; then
    # Kitty clones an SSH split only when its foreground cmdline is kitten ssh
    # and the window has a reported cwd. A direct child stays on Kitty's PTY
    # (REMNIX_PTY_PROXY_MODE=kitty). The height-limited panel is a Kitty overlay.
    if [[ -n "${REMNIX_PTY_PROXY_DISABLE:-}" ]]; then
      unset REMNIX_PTY_PROXY_MODE
    elif [[ -z "${REMNIX_PTY_PROXY_FORCE:-}" && -n "${KITTY_WINDOW_ID:-}" && -n "${KITTY_PID:-}" && -z "${TMUX:-}" && "${PPID:-}" == "${KITTY_PID}" ]]; then
      export REMNIX_PTY_PROXY_MODE=kitty
    else
    unset REMNIX_PTY_PROXY_MODE
    export REMNIX_PTY_PROXY_ACTIVE=1
    export REMNIX_SESSION_ACTIVE=1
    export REMNIX_PTY_PROXY_TMUX="$_remnix_pty_tmux_current"
    unset REMNIX_PTY_PROXY_TTY
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
  elif [[ -n "$_remnix_pty_tty" ]]; then
    export REMNIX_PTY_PROXY_TTY="$_remnix_pty_tty"
  fi
  unset _remnix_need_wrap _remnix_pty_tmux_current _remnix_pty_tmux_previous _remnix_pty_tty _remnix_pty_tty_previous _remnix_pty_zsh
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
  set -l _remnix_pty_tty (command tty </dev/tty 2>/dev/null)
  set -l _remnix_pty_tty_previous ""
  if set -q REMNIX_PTY_PROXY_TTY
    set _remnix_pty_tty_previous "$REMNIX_PTY_PROXY_TTY"
  end
  # exec fish must not nest another attach on this PTY. Wrap again for a
  # new tmux pane, or any other nested slave (nvim :terminal).
  set -l _remnix_need_wrap 0
  if not set -q REMNIX_PTY_PROXY_ACTIVE; and not set -q REMNIX_SESSION_ACTIVE
    set _remnix_need_wrap 1
  else if test -n "$_remnix_pty_tmux_current"; and test "$_remnix_pty_tmux_current" != "$_remnix_pty_tmux_previous"
    set _remnix_need_wrap 1
  else if test -n "$_remnix_pty_tty_previous"; and test -n "$_remnix_pty_tty"; and test "$_remnix_pty_tty" != "$_remnix_pty_tty_previous"
    set _remnix_need_wrap 1
  end
  if test "$_remnix_need_wrap" = 1
    # Direct Kitty child: stay on Kitty's PTY. See the posix preamble.
    set -l _remnix_ppid (ps -o ppid= -p $fish_pid 2>/dev/null | string trim)
    set -l _remnix_force ""
    if set -q REMNIX_PTY_PROXY_FORCE
      set _remnix_force "$REMNIX_PTY_PROXY_FORCE"
    end
    if set -q REMNIX_PTY_PROXY_DISABLE; and test -n "$REMNIX_PTY_PROXY_DISABLE"
      set -e REMNIX_PTY_PROXY_MODE
    else if test -z "$_remnix_force"; and set -q KITTY_WINDOW_ID; and set -q KITTY_PID; and not set -q TMUX; and test "$_remnix_ppid" = "$KITTY_PID"
      set -gx REMNIX_PTY_PROXY_MODE kitty
    else
    set -e REMNIX_PTY_PROXY_MODE
    set -gx REMNIX_PTY_PROXY_ACTIVE 1
    set -gx REMNIX_SESSION_ACTIVE 1
    set -gx REMNIX_PTY_PROXY_TMUX "$_remnix_pty_tmux_current"
    set -e REMNIX_PTY_PROXY_TTY
    if not set -q REMNIX_PTY_PROXY_LEGACY; and test -x ` + qa + `
      exec ` + qa + ` --shell (status fish-path) --remnix ` + qb + ` </dev/tty; or true
    end
    exec ` + qb + ` pty-proxy --shell (status fish-path) </dev/tty; or true
    end
  else if test -n "$_remnix_pty_tty"
    set -gx REMNIX_PTY_PROXY_TTY "$_remnix_pty_tty"
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
  let tty_current = (try { ^tty } catch { "" } | str trim)
  let tty_previous = ($env.REMNIX_PTY_PROXY_TTY? | default "")
  if (($env.REMNIX_PTY_PROXY_ACTIVE? | default "") | is-empty) or ($tmux_current != $tmux_previous) or ($tty_previous != "" and $tty_current != "" and $tty_current != $tty_previous) {
    let ppid = (try { ^ps -o ppid= -p $nu.pid | str trim } catch { "" })
    let kitty_direct = (($env.REMNIX_PTY_PROXY_FORCE? | default "") == "") and (($env.KITTY_WINDOW_ID? | default "") != "") and (($env.KITTY_PID? | default "") != "") and (($env.TMUX? | default "") == "") and ($ppid == ($env.KITTY_PID? | default "no-pid"))
    if ($env.REMNIX_PTY_PROXY_DISABLE? | default "") != "" {
      hide-env -i REMNIX_PTY_PROXY_MODE
    } else if $kitty_direct {
      $env.REMNIX_PTY_PROXY_MODE = "kitty"
    } else {
    hide-env -i REMNIX_PTY_PROXY_MODE
    $env.REMNIX_PTY_PROXY_ACTIVE = "1"
    $env.REMNIX_SESSION_ACTIVE = "1"
    $env.REMNIX_PTY_PROXY_TMUX = $tmux_current
    hide-env -i REMNIX_PTY_PROXY_TTY
    if ($env.REMNIX_PTY_PROXY_LEGACY? | default "") == "" and (` + qa + ` | path exists) {
      try { exec ` + qa + ` --shell $nu.current-exe --remnix ` + qb + ` } catch { }
    }
    try { exec ` + qb + ` pty-proxy --shell $nu.current-exe } catch { }
    }
  } else if $tty_current != "" {
    $env.REMNIX_PTY_PROXY_TTY = $tty_current
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
