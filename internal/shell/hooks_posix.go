package shell

import (
	"fmt"
	"strings"
)

func bash(bin string, opts Options) string {
	q := zshQuote(bin)
	attach := opts.AttachBin
	if attach == "" {
		attach = "remnix-attach"
	}
	aq := zshQuote(attach)
	extra := ""
	if opts.SuggestMenu {
		extra += `
__remnix_suggest_menu() {
  local selected
  if ! __remnix_overlay suggest-interactive "$READLINE_LINE"; then
    __remnix_widget_run suggest --interactive --prefix "$READLINE_LINE" --cwd "$PWD" || return
  fi
  selected=$REPLY
  if [[ "$selected" == __remnix_accept__:* ]]; then
    READLINE_LINE="${selected#__remnix_accept__:}"
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
bind -x '"\C-x\C-s": __remnix_suggest_menu'
bind '"\C-@": "\C-x\C-s\C-x\C-n"'
`
	}
	if opts.SuggestEnabled {
		extra += `
if [[ -n ${BLE_VERSION-} ]] && declare -f bleopt >/dev/null 2>&1; then
  function ble/complete/auto-complete/source:remnix {
    local prefix=${_ble_edit_str-} suggestion
    [[ -n $prefix ]] || return 1
    if __remnix_rpc suggest "$prefix" "$PWD"; then
      suggestion=$REPLY
    else
      suggestion=$("$__remnix_bin" suggest --prefix "$prefix" --cwd "$PWD" 2>/dev/null) || return 1
    fi
    [[ $suggestion == "$prefix"* && $suggestion != "$prefix" ]] || return 1
    ble/complete/auto-complete/enter-source "$suggestion" 2>/dev/null || return 1
  }
  bleopt complete_auto_complete_source=remnix:history 2>/dev/null || true
fi
`
	}
	return fmt.Sprintf(`# remnix bash integration
# Add to ~/.bashrc: eval "$(%s init bash)"

__remnix_bin=%s
__remnix_attach=%s
__remnix_session="${__remnix_session:-$$-$(date +%%s)}"

__remnix_widget_run() {
  local st
  REPLY=$("$__remnix_bin" "$@" 3>&1 1>&2 2>&3 3>&-)
  st=$?
  return $st
}

__remnix_overlay() {
  local op=$1 query=$2
  [[ -n ${REMNIX_SESSION_ID:-} ]] || return 1
  if __remnix_rpc "$op" "$query" "$PWD" "$REMNIX_SESSION_ID"; then
    return 0
  fi
  if [[ -n ${__remnix_attach:-} ]]; then
    REPLY=$("$__remnix_attach" --rpc "$op" "$query" "$PWD" "$REMNIX_SESSION_ID" 2>/dev/null) || return 1
    return 0
  fi
  return 1
}

__remnix_agent_reset() {
  unset __remnix_agent __remnix_agent_PID __remnix_fd
}

__remnix_agent_ensure() {
  if [[ -n ${REMNIX_CONTROL_FD:-} ]]; then
    __remnix_fd=$REMNIX_CONTROL_FD
    return 0
  fi
  if [[ -n ${__remnix_fd:-} ]]; then
    return 0
  fi
  "$__remnix_bin" daemon >/dev/null 2>&1 &
  return 0
}

__remnix_rpc() {
  local op=$1
  shift
  if [[ -n ${REMNIX_CONTROL_FD:-} ]]; then
    printf '%%s\0' "$op" "$@" >&"${REMNIX_CONTROL_FD}" || return 1
    local st
    IFS= read -r -d $'\0' st <&"${REMNIX_CONTROL_FD}" || return 1
    IFS= read -r -d $'\0' REPLY <&"${REMNIX_CONTROL_FD}" || return 1
    [[ $st == ok ]]
    return
  fi
  return 1
}

__remnix_preexec() {
  if [[ -n "${COMP_LINE:-}" ]]; then
    return
  fi
  local cmd
  cmd="$(HISTTIMEFORMAT= history 1 2>/dev/null | sed 's/^ *[0-9]* *//')"
  [[ -z "$cmd" || "$cmd" == "${__remnix_last_cmd:-}" ]] && return
  __remnix_last_cmd="$cmd"
  if __remnix_rpc start "$cmd" "$PWD" "$__remnix_session" bash; then
    __remnix_id="$REPLY"
    return
  fi
  __remnix_id="$("$__remnix_bin" history start --command "$cmd" --cwd "$PWD" --session "$__remnix_session" --shell bash 2>/dev/null)" || true
}

__remnix_precmd() {
  local code=$?
  if [[ -n "${__remnix_id:-}" ]]; then
    __remnix_rpc end "$__remnix_id" "$code" || "$__remnix_bin" history end --id "$__remnix_id" --exit "$code" >/dev/null 2>&1 || true
    unset __remnix_id
  fi
}

__remnix_search() {
  local selected
  if ! __remnix_overlay search-interactive "$READLINE_LINE"; then
    __remnix_widget_run search --interactive --query "$READLINE_LINE" --cwd "$PWD" || return
  fi
  selected=$REPLY
  if [[ "$selected" == __remnix_accept__:* ]]; then
    READLINE_LINE="${selected#__remnix_accept__:}"
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

trap '__remnix_preexec' DEBUG
if [[ "${PROMPT_COMMAND:-}" != *__remnix_precmd* ]]; then
  PROMPT_COMMAND="__remnix_precmd${PROMPT_COMMAND:+;$PROMPT_COMMAND}"
fi
bind '"\C-x\C-n": ""'
bind -x '"\C-x\C-r": __remnix_search'
bind '"\C-r": "\C-x\C-r\C-x\C-n"'
__remnix_agent_ensure >/dev/null 2>&1 || true
%s`, bin, q, aq, extra)
}

func fish(bin string, opts Options) string {
	attach := opts.AttachBin
	if attach == "" {
		attach = "remnix-attach"
	}
	extra := ""
	if opts.SuggestMenu {
		extra = `
function remnix-suggest-menu
    set -l query (commandline --current-buffer)
    commandline --current-buffer --replace -- ''
    if not __remnix_overlay_rpc suggest-interactive "$query" "$PWD"
        __remnix_widget_run suggest --interactive --prefix "$query" --cwd "$PWD"
        or begin
            commandline --current-buffer --replace -- "$query"
            commandline -f repaint
            return
        end
    end
    __remnix_apply_selected "$query"
end
__remnix_bind ctrl-space remnix-suggest-menu
`
	}
	return fmt.Sprintf(`# remnix fish integration
# Add to ~/.config/fish/config.fish: %s init fish | source

set -g __remnix_bin %s
set -g __remnix_attach %s
if not set -q __remnix_session
    set -g __remnix_session "$fish_pid"-(date +%%s)
end

function __remnix_bind
    bind $argv
    bind -M insert $argv
end

# Command substitution steals stdout, so the TUI never owns the TTY (zsh
# $(...) keeps stdin/stderr as the terminal; fish does not). Run as a
# foreground command, not a (...) capture, and attach /dev/tty: fish bind
# functions capture stdout, which would skip the overlay and force alt-screen.
# With pty-proxy, /dev/tty is the inner slave (Ctty).
function __remnix_widget_run
    set -l tmp (mktemp)
    or return 1
    $__remnix_bin $argv --result-file $tmp </dev/tty >/dev/tty
    set -l st $status
    set -g __remnix_widget_out ''
    if test -f $tmp
        set -g __remnix_widget_out (string collect -- < $tmp | string trim)
        command rm -f -- $tmp
    end
    return $st
end

function __remnix_apply_selected
    set -l fallback $argv[1]
    set -l selected (string collect -- $__remnix_widget_out | string trim)
    set -l run 0
    if string match -q '__remnix_accept__:*' -- $selected
        set selected (string replace -r '^__remnix_accept__:' '' -- $selected)
        set run 1
    end
    if test -z "$selected"
        commandline --current-buffer --replace -- "$fallback"
        commandline -f repaint
        return
    end
    # Wipe first: after a blocking TUI, fish 4 can keep the pre-widget
    # buffer and -r alone concatenates (rm + rm -rf → rmrm -rf).
    commandline --current-buffer --replace -- ''
    commandline --current-buffer --replace -- "$selected"
    commandline -f repaint
    if test $run -eq 1
        commandline -f execute
    end
end

function __remnix_agent_ensure
    $__remnix_bin daemon >/dev/null 2>&1 &
end

function __remnix_rpc
    if not set -q REMNIX_CONTROL_FD
        return 1
    end
    printf '%%s\0' $argv >&$REMNIX_CONTROL_FD
    or return 1
    set -l st
    read --null st <&$REMNIX_CONTROL_FD
    or return 1
    read --null -g REPLY <&$REMNIX_CONTROL_FD
    or return 1
    test "$st" = ok
end

function __remnix_overlay_rpc
    set -l op $argv[1]
    set -l query $argv[2]
    set -l cwd $argv[3]
    if not set -q REMNIX_SESSION_ID
        return 1
    end
    if __remnix_rpc $op $query $cwd $REMNIX_SESSION_ID
        set -g __remnix_widget_out $REPLY
        return 0
    end
    set -l tmp (mktemp)
    or return 1
    $__remnix_attach --rpc $op $query $cwd $REMNIX_SESSION_ID >$tmp 2>/dev/null
    set -l st $status
    set -g __remnix_widget_out (string collect -- < $tmp | string trim)
    command rm -f -- $tmp
    test $st -eq 0
end

function __remnix_preexec --on-event fish_preexec
    if __remnix_rpc start "$argv[1]" "$PWD" "$__remnix_session" fish
        set -g __remnix_id $REPLY
        return
    end
    set -g __remnix_id ($__remnix_attach --rpc start "$argv[1]" "$PWD" "$__remnix_session" fish 2>/dev/null)
    or set -g __remnix_id ($__remnix_bin history start --command "$argv[1]" --cwd "$PWD" --session "$__remnix_session" --shell fish 2>/dev/null)
end

function __remnix_postexec --on-event fish_postexec
    set -l code $status
    if set -q __remnix_id
        $__remnix_attach --rpc end "$__remnix_id" "$code" >/dev/null 2>&1
        or $__remnix_bin history end --id "$__remnix_id" --exit $code >/dev/null 2>&1
        set -e __remnix_id
    end
end

function remnix-search
    set -l query (commandline --current-buffer)
    commandline --current-buffer --replace -- ''
    if not __remnix_overlay_rpc search-interactive "$query" "$PWD"
        __remnix_widget_run search --interactive --query "$query" --cwd "$PWD"
        or begin
            commandline --current-buffer --replace -- "$query"
            commandline -f repaint
            return
        end
    end
    __remnix_apply_selected "$query"
end
__remnix_bind \cr remnix-search
__remnix_agent_ensure
%s`, bin, zshQuote(bin), zshQuote(attach), extra)
}

func nu(bin string, opts Options) string {
	q := strings.ReplaceAll(bin, `\`, `\\`)
	q = strings.ReplaceAll(q, `"`, `\"`)
	attach := opts.AttachBin
	if attach == "" {
		attach = "remnix-attach"
	}
	aq := strings.ReplaceAll(attach, `\`, `\\`)
	aq = strings.ReplaceAll(aq, `"`, `\"`)
	menu := ""
	if opts.SuggestMenu {
		menu = `
def --env remnix-suggest-menu [] {
  let query = (commandline)
  commandline edit --replace ""
  let selected = (__remnix_overlay_or_tui "suggest-interactive" $query)
  if ($selected | str starts-with "__remnix_err__") {
    commandline edit --replace $query
    return
  }
  if ($selected | str starts-with "__remnix_accept__:") {
    commandline edit --replace --accept ($selected | str replace -r '^__remnix_accept__:' '')
  } else if not ($selected | is-empty) {
    commandline edit --replace $selected
  } else {
    commandline edit --replace $query
  }
}
__remnix_rebind {
  name: remnix_suggest_menu
  modifier: control
  keycode: space
  mode: [emacs, vi_insert, vi_normal]
  event: { send: executehostcommand, cmd: "remnix-suggest-menu" }
}
`
	}
	return fmt.Sprintf(`# remnix nushell integration
# source is parse-time: it cannot see a file written later in the same script.
# Generate from env.nu (evaluated before config.nu is parsed):
#   mkdir ~/.cache
#   ^remnix init nu | save --force ~/.cache/remnix.nu
# Then this literal line at the top of config.nu (required if pty_proxy is on):
#   source ~/.cache/remnix.nu

let __remnix_bin = "%[1]s"
let __remnix_attach = "%[3]s"
$env.__remnix_session = ($env.__remnix_session? | default $"($nu.pid)-(date now | format date '%%s')")

do { ^$"($__remnix_bin)" daemon } | ignore

# Replace an existing modifier+keycode binding (Nushell's default Ctrl+R is
# history_menu; appending a second Ctrl+R leaves that grid in place).
def --env __remnix_rebind [binding: record] {
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

def __remnix_overlay_or_tui [op: string, query: string] {
  if ($env.REMNIX_SESSION_ID? | default "") != "" {
    let rpc = (do { ^$"($__remnix_attach)" --rpc $op $query $env.PWD $env.REMNIX_SESSION_ID } | complete)
    if $rpc.exit_code == 0 {
      return ($rpc.stdout | str trim)
    }
  }
  let tmp = (^mktemp | str trim)
  let ran = (
    if $op == "suggest-interactive" {
      do { ^$"($__remnix_bin)" suggest --interactive --prefix $query --cwd $env.PWD --result-file $tmp o> /dev/tty e> /dev/tty } | complete
    } else {
      do { ^$"($__remnix_bin)" search --interactive --query $query --cwd $env.PWD --result-file $tmp o> /dev/tty e> /dev/tty } | complete
    }
  )
  let selected = (try { open --raw $tmp } catch { "" } | str trim)
  try { rm $tmp }
  if $ran.exit_code != 0 {
    return "__remnix_err__"
  }
  $selected
}

$env.config = ($env.config | default {} | upsert hooks {|c|
  let hooks = ($c.hooks? | default {})
  $hooks
  | upsert pre_execution {|h|
      ($h.pre_execution? | default []) | append {||
        let cmd = (commandline)
        if not ($cmd | is-empty) {
          if ($env.REMNIX_CONTROL_FD? | default "") != "" {
            $env.__remnix_id = (^$"($__remnix_attach)" --rpc start $cmd $env.PWD $env.__remnix_session nu | str trim)
          } else {
            $env.__remnix_id = (^$"($__remnix_attach)" --rpc start $cmd $env.PWD $env.__remnix_session nu | str trim)
          }
        }
      }
    }
  | upsert pre_prompt {|h|
      ($h.pre_prompt? | default []) | append {||
        if ($env.__remnix_id? | default "") != "" {
          ^$"($__remnix_attach)" --rpc end $env.__remnix_id $env.LAST_EXIT_CODE
          hide-env -i __remnix_id
        }
      }
    }
})

__remnix_rebind {
  name: remnix_search
  modifier: control
  keycode: char_r
  mode: [emacs, vi_insert, vi_normal]
  event: { send: executehostcommand, cmd: "remnix-search" }
}

def remnix-search [] {
  let query = (commandline)
  commandline edit --replace ""
  let selected = (__remnix_overlay_or_tui "search-interactive" $query)
  if ($selected | str starts-with "__remnix_err__") {
    commandline edit --replace $query
    return
  }
  if ($selected | str starts-with "__remnix_accept__:") {
    commandline edit --replace --accept ($selected | str replace -r '^__remnix_accept__:' '')
  } else if not ($selected | is-empty) {
    commandline edit --replace $selected
  } else {
    commandline edit --replace $query
  }
}
%[2]s`, q, menu, aq)
}
