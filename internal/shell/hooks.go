package shell

import (
	"fmt"
	"strings"
)

type Options struct {
	SuggestEnabled bool
	SuggestAccept  []string
}

func Integration(shellName, binary string, opts Options) (string, error) {
	if binary == "" {
		binary = "syncsh"
	}
	switch strings.ToLower(shellName) {
	case "zsh":
		return zsh(binary, opts), nil
	case "bash":
		return bash(binary), nil
	case "fish":
		return fish(binary), nil
	default:
		return "", fmt.Errorf("unsupported shell %q (want zsh, bash, or fish)", shellName)
	}
}

func zsh(bin string, opts Options) string {
	suggest := ""
	if opts.SuggestEnabled {
		suggest = zshSuggest(opts.SuggestAccept)
	}
	return fmt.Sprintf(`# syncsh zsh integration
# Add to ~/.zshrc: eval "$(%s init zsh)"

typeset -g __syncsh_bin=%s
typeset -g __syncsh_session="${__syncsh_session:-$$-$(date +%%s)}"
typeset -g __syncsh_fd=""
typeset -g __syncsh_out=""
typeset -g __syncsh_in=""

__syncsh_sock() {
  if [[ -n ${SYNCSH_RUNTIME_DIR:-} ]]; then
    print -r -- "$SYNCSH_RUNTIME_DIR/agent.sock"
  elif [[ -n ${XDG_RUNTIME_DIR:-} ]]; then
    print -r -- "$XDG_RUNTIME_DIR/syncsh/agent.sock"
  else
    print -r -- "${TMPDIR:-/tmp}/syncsh/agent.sock"
  fi
}

__syncsh_agent_reset() {
  unset __syncsh_fd __syncsh_out __syncsh_in
  __syncsh_fd=""
  __syncsh_out=""
  __syncsh_in=""
}

__syncsh_agent_connect() {
  emulate -L zsh
  [[ -n ${__syncsh_fd:-} || -n ${__syncsh_out:-} ]] && return 0
  local sock
  sock="$(__syncsh_sock)"
  if zmodload zsh/net/socket 2>/dev/null && [[ -S $sock ]] && zsocket "$sock" 2>/dev/null; then
    __syncsh_fd=$REPLY
    return 0
  fi
  return 1
}

__syncsh_agent_ensure() {
  emulate -L zsh
  __syncsh_agent_connect && return 0
  "$__syncsh_bin" agent >/dev/null 2>&1 &!
  local i
  for i in {1..20}; do
    __syncsh_agent_connect && return 0
    zmodload zsh/zselect 2>/dev/null && zselect -t 1 || sleep 0.01
  done
  if [[ -z ${__syncsh_out:-} ]]; then
    coproc { "$__syncsh_bin" agent --stdio }
    __syncsh_out=${COPROC[1]}
    __syncsh_in=${COPROC[2]}
  fi
  [[ -n ${__syncsh_out:-} ]]
}

__syncsh_rpc() {
  emulate -L zsh
  __syncsh_agent_ensure || return 1
  local op=$1 f out in
  shift
  out=${__syncsh_out:-$__syncsh_fd}
  in=${__syncsh_in:-$__syncsh_fd}
  print -n -u $out -- "$op"$'\0' || { __syncsh_agent_reset; return 1 }
  for f in "$@"; do
    print -n -u $out -- "$f"$'\0' || { __syncsh_agent_reset; return 1 }
  done
  local st
  IFS= read -r -d $'\0' -u $in st || { __syncsh_agent_reset; return 1 }
  IFS= read -r -d $'\0' -u $in REPLY || { __syncsh_agent_reset; return 1 }
  [[ $st == ok ]]
}

__syncsh_preexec() {
  local cmd="${1:-}"
  [[ -z "$cmd" ]] && return
  if __syncsh_rpc start "$cmd" "$PWD" "$__syncsh_session" zsh; then
    __syncsh_id="$REPLY"
    return
  fi
  __syncsh_id="$("$__syncsh_bin" history start --command "$cmd" --cwd "$PWD" --session "$__syncsh_session" --shell zsh 2>/dev/null)" || true
}

__syncsh_precmd() {
  local code=$?
  if [[ -n "${__syncsh_id:-}" ]]; then
    __syncsh_rpc end "$__syncsh_id" "$code" || "$__syncsh_bin" history end --id "$__syncsh_id" --exit "$code" >/dev/null 2>&1 || true
    unset __syncsh_id
  fi
}

syncsh-search() {
  local selected run=0
  selected="$("$__syncsh_bin" search --interactive --query "$LBUFFER" --cwd "$PWD" </dev/tty)" || return
  if [[ "$selected" == __syncsh_accept__:* ]]; then
    selected="${selected#__syncsh_accept__:}"
    run=1
  fi
  if [[ -n "$selected" ]]; then
    LBUFFER="$selected"
    RBUFFER=""
    unset POSTDISPLAY
  fi
  zle reset-prompt
  # Use the builtin. Nested "zle accept-line" runs the suggest wrapper with
  # WIDGET still set to syncsh-search, so orig lookup is empty → "No such widget".
  (( run )) && zle .accept-line
}

zle -N syncsh-search
bindkey '^R' syncsh-search
bindkey -M viins '^R' syncsh-search

autoload -Uz add-zsh-hook
add-zsh-hook preexec __syncsh_preexec
add-zsh-hook precmd __syncsh_precmd
__syncsh_agent_ensure >/dev/null 2>&1 || true
%s`, bin, zshQuote(bin), suggest)
}

func zshSuggest(accept []string) string {
	if len(accept) == 0 {
		accept = []string{"Right"}
	}
	quoted := make([]string, len(accept))
	for i, k := range accept {
		quoted[i] = zshQuote(k)
	}
	return fmt.Sprintf(`
# inline history suggestions (ghost text)
typeset -ga __syncsh_suggest_accept
__syncsh_suggest_accept=(%s)
typeset -gA __syncsh_suggest_fallback
typeset -g __syncsh_suggest_last=""
# zsh 5.9 ignores "faint"; 238 is a muted gray that actually recedes
typeset -g __syncsh_suggest_hl=fg=238
zle_highlight=(${zle_highlight:#suffix:*})
zle_highlight+=(suffix:${__syncsh_suggest_hl})

# Color POSTDISPLAY with both suffix (zle_highlight) and region_highlight.
# suffix alone is ignored by some zsh/theme combos and the ghost goes white.
# Enter still drops POSTDISPLAY before accept-line, so the region does not
# get committed as part of the command.
__syncsh_suggest_highlight() {
  region_highlight=(${region_highlight:#*memo=syncsh-suggest*})
  if [[ -n ${POSTDISPLAY:-} ]]; then
    region_highlight+=("${#BUFFER} $(( $#BUFFER + $#POSTDISPLAY )) ${__syncsh_suggest_hl} memo=syncsh-suggest")
  fi
}

__syncsh_suggest_clear() {
  unset POSTDISPLAY
  __syncsh_suggest_highlight
}

__syncsh_suggest_update() {
  emulate -L zsh
  # POSTDISPLAY is appended after BUFFER, not the cursor. Matching on
  # LBUFFER makes the ghost overlap RBUFFER as soon as the cursor moves left.
  if [[ -z $BUFFER ]]; then
    unset POSTDISPLAY
    __syncsh_suggest_last=""
    __syncsh_suggest_highlight
    return
  fi
  if [[ ${__syncsh_suggest_last} == "$BUFFER" ]]; then
    __syncsh_suggest_highlight
    return
  fi
  __syncsh_suggest_last="$BUFFER"
  local s=""
  if __syncsh_rpc suggest "$BUFFER" "$PWD"; then
    s="$REPLY"
  else
    s="$("$__syncsh_bin" suggest --prefix "$BUFFER" --cwd "$PWD" 2>/dev/null)" || s=""
  fi
  if [[ -n $s && $s == "$BUFFER"* && $s != "$BUFFER" ]]; then
    POSTDISPLAY="${s#"$BUFFER"}"
  else
    unset POSTDISPLAY
  fi
  __syncsh_suggest_highlight
}

__syncsh_suggest_redraw() {
  case $WIDGET in
    accept-line|accept-and-hold|accept-line-and-down-history|accept-and-infer-next-history)
      __syncsh_suggest_clear
      return
      ;;
  esac
  __syncsh_suggest_update
}

__syncsh_suggest_bind_clear() {
  emulate -L zsh
  local w orig
  for w in accept-line accept-and-hold accept-line-and-down-history accept-and-infer-next-history; do
    [[ ${widgets[$w]:-} == user:__syncsh_suggest_clear_then_$w ]] && continue
    orig="__syncsh_suggest_orig_$w"
    case "${widgets[$w]:-}" in
      user:*)
        zle -A "$w" "$orig"
        ;;
      builtin|'')
        eval "$orig() { zle .$w }"
        zle -N "$orig"
        ;;
      *)
        continue
        ;;
    esac
    # One wrapper per widget. A shared wrapper keyed on $WIDGET breaks when
    # another widget (syncsh-search) invokes accept-line: $WIDGET stays the caller.
    eval "__syncsh_suggest_clear_then_$w() {
      emulate -L zsh
      __syncsh_suggest_clear
      zle $orig
    }"
    zle -N "$w" "__syncsh_suggest_clear_then_$w"
  done
}

syncsh-suggest-accept() {
  emulate -L zsh
  local fb="${__syncsh_suggest_fallback[$KEYS]:-forward-char}"
  if [[ -n ${POSTDISPLAY:-} ]]; then
    if [[ $fb == forward-char && $CURSOR -ne $#BUFFER ]]; then
      zle "$fb"
      return
    fi
    BUFFER="$BUFFER$POSTDISPLAY"
    unset POSTDISPLAY
    CURSOR=$#BUFFER
    __syncsh_suggest_last="$BUFFER"
    zle redisplay
    return
  fi
  zle "$fb"
}

zle -N syncsh-suggest-accept
zle -N __syncsh_suggest_redraw

__syncsh_suggest_bind() {
  emulate -L zsh
  local spec seq fb
  for spec in "${__syncsh_suggest_accept[@]}"; do
    case "${(L)spec}" in
      right|arrow-right|rightarrow|forward-char)
        fb=forward-char
        for seq in "${terminfo[kcuf1]:-}" $'\e[C' $'\eOC'; do
          [[ -n $seq ]] || continue
          __syncsh_suggest_fallback[$seq]=$fb
          bindkey "$seq" syncsh-suggest-accept
        done
        ;;
      tab)
        fb=expand-or-complete
        __syncsh_suggest_fallback[$'\t']=$fb
        bindkey $'\t' syncsh-suggest-accept
        ;;
      end)
        fb=end-of-line
        for seq in "${terminfo[kend]:-}" $'\e[F' $'\eOF'; do
          [[ -n $seq ]] || continue
          __syncsh_suggest_fallback[$seq]=$fb
          bindkey "$seq" syncsh-suggest-accept
        done
        ;;
      c-e|ctrl-e|^e)
        fb=end-of-line
        __syncsh_suggest_fallback[$'^E']=$fb
        bindkey '^E' syncsh-suggest-accept
        ;;
      c-f|ctrl-f|^f)
        fb=forward-char
        __syncsh_suggest_fallback[$'^F']=$fb
        bindkey '^F' syncsh-suggest-accept
        ;;
      *)
        fb=forward-char
        __syncsh_suggest_fallback[$spec]=$fb
        bindkey -- "$spec" syncsh-suggest-accept
        ;;
    esac
  done
}

__syncsh_suggest_bind
__syncsh_suggest_bind_clear

if autoload -Uz add-zle-hook-widget 2>/dev/null && add-zle-hook-widget line-pre-redraw __syncsh_suggest_redraw 2>/dev/null; then
  :
else
  __syncsh_suggest_self_insert() {
    zle .self-insert
    __syncsh_suggest_update
    zle -R
  }
  __syncsh_suggest_backward_delete() {
    zle .backward-delete-char
    __syncsh_suggest_last=""
    __syncsh_suggest_update
    zle -R
  }
  zle -N self-insert __syncsh_suggest_self_insert
  zle -N backward-delete-char __syncsh_suggest_backward_delete
fi
`, strings.Join(quoted, " "))
}

func zshQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func bash(bin string) string {
	return fmt.Sprintf(`# syncsh bash integration
# Add to ~/.bashrc: eval "$(%s init bash)"

__syncsh_session="${__syncsh_session:-$$-$(date +%%s)}"

__syncsh_preexec() {
  if [[ -n "${COMP_LINE:-}" ]]; then
    return
  fi
  local cmd
  cmd="$(HISTTIMEFORMAT= history 1 2>/dev/null | sed 's/^ *[0-9]* *//')"
  [[ -z "$cmd" || "$cmd" == "${__syncsh_last_cmd:-}" ]] && return
  __syncsh_last_cmd="$cmd"
  __syncsh_id="$(%s history start --command "$cmd" --cwd "$PWD" --session "$__syncsh_session" --shell bash 2>/dev/null)" || true
}

__syncsh_precmd() {
  local code=$?
  if [[ -n "${__syncsh_id:-}" ]]; then
    %s history end --id "$__syncsh_id" --exit "$code" >/dev/null 2>&1 || true
    unset __syncsh_id
  fi
}

__syncsh_search() {
  local selected
  selected="$(%s search --interactive --query "$READLINE_LINE" --cwd "$PWD" </dev/tty)" || return
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
`, bin, bin, bin, bin)
}

func fish(bin string) string {
	return fmt.Sprintf(`# syncsh fish integration
# Add to ~/.config/fish/config.fish: %s init fish | source

if not set -q __syncsh_session
    set -g __syncsh_session "$fish_pid"-(date +%%s)
end

function __syncsh_preexec --on-event fish_preexec
    set -g __syncsh_id (%s history start --command "$argv[1]" --cwd "$PWD" --session "$__syncsh_session" --shell fish 2>/dev/null)
end

function __syncsh_postexec --on-event fish_postexec
    set -l code $status
    if set -q __syncsh_id
        %s history end --id "$__syncsh_id" --exit $code >/dev/null 2>&1
        set -e __syncsh_id
    end
end

function syncsh-search
    set -l selected
    %s search --interactive --query (commandline -b) --cwd "$PWD" </dev/tty | read -l selected
    if string match -q '__syncsh_accept__:*' -- "$selected"
        set selected (string replace -r '^__syncsh_accept__:' '' -- "$selected")
        commandline -r -- "$selected"
        commandline -f execute
    else if test -n "$selected"
        commandline -r -- "$selected"
        commandline -f repaint
    end
end
bind \cr syncsh-search
`, bin, bin, bin, bin)
}
