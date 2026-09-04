package shell

import (
	"fmt"
	"strings"
)

func bash(bin string, opts Options) string {
	q := zshQuote(bin)
	extra := ""
	if opts.SuggestMenu {
		extra += `
__syncsh_suggest_menu() {
  local selected
  __syncsh_widget_run suggest --interactive --prefix "$READLINE_LINE" --cwd "$PWD" || return
  selected=$REPLY
  if [[ "$selected" == __syncsh_accept__:* ]]; then
    READLINE_LINE="${selected#__syncsh_accept__:}"
    READLINE_POINT=${#READLINE_LINE}
    bind '"\C-x\C-n": accept-line'
  elif [[ -n "$selected" ]]; then
    READLINE_LINE="$selected"
    READLINE_POINT=${#READLINE_LINE}
    bind '"\C-x\C-n": ""'
  else
    bind '"\C-x\C-n": ""'
  fi
}
bind -x '"\C-x\C-s": __syncsh_suggest_menu'
bind '"\C-@": "\C-x\C-s\C-x\C-n"'
`
	}
	if opts.SuggestEnabled {
		extra += `
if [[ -n ${BLE_VERSION-} ]] && declare -f bleopt >/dev/null 2>&1; then
  function ble/complete/auto-complete/source:syncsh {
    local prefix=${_ble_edit_str-} suggestion
    [[ -n $prefix ]] || return 1
    if __syncsh_rpc suggest "$prefix" "$PWD"; then
      suggestion=$REPLY
    else
      suggestion=$("$__syncsh_bin" suggest --prefix "$prefix" --cwd "$PWD" 2>/dev/null) || return 1
    fi
    [[ $suggestion == "$prefix"* && $suggestion != "$prefix" ]] || return 1
    ble/complete/auto-complete/enter-source "$suggestion" 2>/dev/null || return 1
  }
  bleopt complete_auto_complete_source=syncsh:history 2>/dev/null || true
fi
`
	}
	return fmt.Sprintf(`# syncsh bash integration
# Add to ~/.bashrc: eval "$(%s init bash)"

__syncsh_bin=%s
__syncsh_session="${__syncsh_session:-$$-$(date +%%s)}"

__syncsh_widget_run() {
  local st
  REPLY=$("$__syncsh_bin" "$@" 3>&1 1>&2 2>&3 3>&-)
  st=$?
  return $st
}

__syncsh_agent_reset() {
  unset __syncsh_agent __syncsh_agent_PID
}

__syncsh_agent_ensure() {
  if [[ -n ${__syncsh_agent_PID:-} ]] && kill -0 "$__syncsh_agent_PID" 2>/dev/null; then
    return 0
  fi
  "$__syncsh_bin" agent >/dev/null 2>&1 &
  local i
  for i in 1 2 3 4 5 6 7 8 9 10; do
    sleep 0.02
  done
  if [[ -z ${__syncsh_agent_PID:-} ]]; then
    coproc __syncsh_agent { "$__syncsh_bin" agent --stdio; }
  fi
  [[ -n ${__syncsh_agent_PID:-} ]]
}

__syncsh_rpc() {
  __syncsh_agent_ensure || return 1
  [[ -n ${__syncsh_agent[1]:-} ]] || return 1
  local op=$1
  shift
  printf '%%s\0' "$op" "$@" >&"${__syncsh_agent[1]}" || { __syncsh_agent_reset; return 1; }
  local st
  IFS= read -r -d $'\0' st <&"${__syncsh_agent[0]}" || { __syncsh_agent_reset; return 1; }
  IFS= read -r -d $'\0' REPLY <&"${__syncsh_agent[0]}" || { __syncsh_agent_reset; return 1; }
  [[ $st == ok ]]
}

__syncsh_preexec() {
  if [[ -n "${COMP_LINE:-}" ]]; then
    return
  fi
  local cmd
  cmd="$(HISTTIMEFORMAT= history 1 2>/dev/null | sed 's/^ *[0-9]* *//')"
  [[ -z "$cmd" || "$cmd" == "${__syncsh_last_cmd:-}" ]] && return
  __syncsh_last_cmd="$cmd"
  if __syncsh_rpc start "$cmd" "$PWD" "$__syncsh_session" bash; then
    __syncsh_id="$REPLY"
    return
  fi
  __syncsh_id="$("$__syncsh_bin" history start --command "$cmd" --cwd "$PWD" --session "$__syncsh_session" --shell bash 2>/dev/null)" || true
}

__syncsh_precmd() {
  local code=$?
  if [[ -n "${__syncsh_id:-}" ]]; then
    __syncsh_rpc end "$__syncsh_id" "$code" || "$__syncsh_bin" history end --id "$__syncsh_id" --exit "$code" >/dev/null 2>&1 || true
    unset __syncsh_id
  fi
}

__syncsh_search() {
  local selected
  __syncsh_widget_run search --interactive --query "$READLINE_LINE" --cwd "$PWD" || return
  selected=$REPLY
  if [[ "$selected" == __syncsh_accept__:* ]]; then
    READLINE_LINE="${selected#__syncsh_accept__:}"
    READLINE_POINT=${#READLINE_LINE}
    bind '"\C-x\C-n": accept-line'
  elif [[ -n "$selected" ]]; then
    READLINE_LINE="$selected"
    READLINE_POINT=${#READLINE_LINE}
    bind '"\C-x\C-n": ""'
  else
    bind '"\C-x\C-n": ""'
  fi
}

trap '__syncsh_preexec' DEBUG
if [[ "${PROMPT_COMMAND:-}" != *__syncsh_precmd* ]]; then
  PROMPT_COMMAND="__syncsh_precmd${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
fi
bind '"\C-x\C-n": ""'
bind -x '"\C-x\C-r": __syncsh_search'
bind '"\C-r": "\C-x\C-r\C-x\C-n"'
__syncsh_agent_ensure >/dev/null 2>&1 || true
%s`, bin, q, extra)
}

func fish(bin string, opts Options) string {
	extra := ""
	if opts.SuggestMenu {
		extra = `
function syncsh-suggest-menu
    __syncsh_widget_run suggest --interactive --prefix (commandline -b) --cwd "$PWD"
    or begin
        commandline -f repaint
        return
    end
    __syncsh_apply_selected
end
__syncsh_bind ctrl-space syncsh-suggest-menu
`
	}
	return fmt.Sprintf(`# syncsh fish integration
# Add to ~/.config/fish/config.fish: %s init fish | source

set -g __syncsh_bin %s
if not set -q __syncsh_session
    set -g __syncsh_session "$fish_pid"-(date +%%s)
end

function __syncsh_bind
    bind $argv
    bind -M insert $argv
end

# Command substitution steals stdout, so the TUI never owns the TTY (zsh
# $(...) keeps stdin/stderr as the terminal; fish does not). Run as a
# foreground command, not a (...) capture, and attach /dev/tty: fish bind
# functions capture stdout, which would skip the overlay and force alt-screen.
# With pty-proxy, /dev/tty is the inner slave (Ctty).
function __syncsh_widget_run
    set -l tmp (mktemp)
    or return 1
    $__syncsh_bin $argv --result-file $tmp </dev/tty >/dev/tty
    set -l st $status
    set -g __syncsh_widget_out ''
    if test -f $tmp
        set -g __syncsh_widget_out (string collect -- < $tmp | string trim)
        command rm -f -- $tmp
    end
    return $st
end

function __syncsh_apply_selected
    set -l selected $__syncsh_widget_out
    if string match -q '__syncsh_accept__:*' -- "$selected"
        set selected (string replace -r '^__syncsh_accept__:' '' -- "$selected")
        commandline -r -- "$selected"
        commandline -f execute
    else if test -n "$selected"
        commandline -r -- "$selected"
    end
    commandline -f repaint
end

function __syncsh_agent_ensure
    $__syncsh_bin agent >/dev/null 2>&1 &
end

function __syncsh_preexec --on-event fish_preexec
    set -g __syncsh_id ($__syncsh_bin history start --command "$argv[1]" --cwd "$PWD" --session "$__syncsh_session" --shell fish 2>/dev/null)
end

function __syncsh_postexec --on-event fish_postexec
    set -l code $status
    if set -q __syncsh_id
        $__syncsh_bin history end --id "$__syncsh_id" --exit $code >/dev/null 2>&1
        set -e __syncsh_id
    end
end

function syncsh-search
    __syncsh_widget_run search --interactive --query (commandline -b) --cwd "$PWD"
    or begin
        commandline -f repaint
        return
    end
    __syncsh_apply_selected
end
__syncsh_bind \cr syncsh-search
__syncsh_agent_ensure
%s`, bin, zshQuote(bin), extra)
}

func nu(bin string, opts Options) string {
	q := strings.ReplaceAll(bin, `\`, `\\`)
	q = strings.ReplaceAll(q, `"`, `\"`)
	menu := ""
	if opts.SuggestMenu {
		menu = `
def --env syncsh-suggest-menu [] {
  let tmp = (^mktemp | str trim)
  ^$"($__syncsh_bin)" suggest --interactive --prefix (commandline) --cwd $env.PWD --result-file $tmp o> /dev/tty e> /dev/tty
  let selected = (try { open --raw $tmp } catch { "" } | str trim)
  try { rm $tmp }
  if ($selected | str starts-with "__syncsh_accept__:") {
    commandline edit --replace ($selected | str replace -r '^__syncsh_accept__:' '')
  } else if not ($selected | is-empty) {
    commandline edit --replace $selected
  }
}
__syncsh_rebind {
  name: syncsh_suggest_menu
  modifier: control
  keycode: space
  mode: [emacs, vi_insert, vi_normal]
  event: { send: executehostcommand, cmd: "syncsh-suggest-menu" }
}
`
	}
	return fmt.Sprintf(`# syncsh nushell integration
# source is parse-time: it cannot see a file written later in the same script.
# Generate from env.nu (evaluated before config.nu is parsed):
#   mkdir ~/.cache
#   ^syncsh init nu | save --force ~/.cache/syncsh.nu
# Then this literal line at the top of config.nu (required if pty_proxy is on):
#   source ~/.cache/syncsh.nu

let __syncsh_bin = "%[1]s"
$env.__syncsh_session = ($env.__syncsh_session? | default $"($nu.pid)-(date now | format date '%%s')")

do { ^$"($__syncsh_bin)" agent } | ignore

# Replace an existing modifier+keycode binding (Nushell's default Ctrl+R is
# history_menu; appending a second Ctrl+R leaves that grid in place).
def --env __syncsh_rebind [binding: record] {
  $env.config = ($env.config | default {} | upsert keybindings {|c|
    let rest = (
      $c.keybindings? | default [] | where {|k|
        not (
          ($k.modifier? | default "") == $binding.modifier
          and ($k.keycode? | default "") == $binding.keycode
        )
      }
    )
    $rest | append $binding
  })
}

$env.config = ($env.config | default {} | upsert hooks {|c|
  let hooks = ($c.hooks? | default {})
  $hooks
  | upsert pre_execution {|h|
      ($h.pre_execution? | default []) | append {||
        let cmd = (commandline)
        if not ($cmd | is-empty) {
          $env.__syncsh_id = (^$"($__syncsh_bin)" history start --command $cmd --cwd $env.PWD --session $env.__syncsh_session --shell nu | str trim)
        }
      }
    }
  | upsert pre_prompt {|h|
      ($h.pre_prompt? | default []) | append {||
        if ($env.__syncsh_id? | default "") != "" {
          ^$"($__syncsh_bin)" history end --id $env.__syncsh_id --exit $env.LAST_EXIT_CODE
          hide-env -i __syncsh_id
        }
      }
    }
})

__syncsh_rebind {
  name: syncsh_search
  modifier: control
  keycode: char_r
  mode: [emacs, vi_insert, vi_normal]
  event: { send: executehostcommand, cmd: "syncsh-search" }
}

def syncsh-search [] {
  let tmp = (^mktemp | str trim)
  ^$"($__syncsh_bin)" search --interactive --query (commandline) --cwd $env.PWD --result-file $tmp o> /dev/tty e> /dev/tty
  let selected = (try { open --raw $tmp } catch { "" } | str trim)
  try { rm $tmp }
  if ($selected | str starts-with "__syncsh_accept__:") {
    commandline edit --replace ($selected | str replace -r '^__syncsh_accept__:' '')
  } else if not ($selected | is-empty) {
    commandline edit --replace $selected
  }
}
%[2]s`, q, menu)
}
