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
		suggest = zshSuggest(bin, opts.SuggestAccept)
	}
	return fmt.Sprintf(`# syncsh zsh integration
# Add to ~/.zshrc: eval "$(%s init zsh)"

__syncsh_session="${__syncsh_session:-$$-$(date +%%s)}"

__syncsh_preexec() {
  local cmd="${1:-}"
  [[ -z "$cmd" ]] && return
  __syncsh_id="$(%s history start --command "$cmd" --cwd "$PWD" --session "$__syncsh_session" --shell zsh 2>/dev/null)" || true
}

__syncsh_precmd() {
  local code=$?
  if [[ -n "${__syncsh_id:-}" ]]; then
    %s history end --id "$__syncsh_id" --exit "$code" >/dev/null 2>&1 || true
    unset __syncsh_id
  fi
}

syncsh-search() {
  local selected run=0
  selected="$(%s search --interactive --query "$LBUFFER" --cwd "$PWD" </dev/tty)" || return
  if [[ "$selected" == __syncsh_accept__:* ]]; then
    selected="${selected#__syncsh_accept__:}"
    run=1
  fi
  if [[ -n "$selected" ]]; then
    LBUFFER="$selected"
    RBUFFER=""
  fi
  zle reset-prompt
  (( run )) && zle accept-line
}

zle -N syncsh-search
bindkey '^R' syncsh-search

autoload -Uz add-zsh-hook
add-zsh-hook preexec __syncsh_preexec
add-zsh-hook precmd __syncsh_precmd
%s`, bin, bin, bin, bin, suggest)
}

func zshSuggest(bin string, accept []string) string {
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

__syncsh_suggest_update() {
  emulate -L zsh
  if [[ -z $LBUFFER ]]; then
    unset POSTDISPLAY
    __syncsh_suggest_last=""
    region_highlight=(${region_highlight:#*memo=syncsh-suggest*})
    return
  fi
  if [[ ${__syncsh_suggest_last} == "$LBUFFER" ]]; then
    return
  fi
  __syncsh_suggest_last="$LBUFFER"
  local s
  s="$(%s suggest --prefix "$LBUFFER" --cwd "$PWD" 2>/dev/null)" || s=""
  if [[ -n $s && $s == "$LBUFFER"* && $s != "$LBUFFER" ]]; then
    POSTDISPLAY="${s#"$LBUFFER"}"
  else
    unset POSTDISPLAY
  fi
  region_highlight=(${region_highlight:#*memo=syncsh-suggest*})
  if [[ -n ${POSTDISPLAY:-} ]]; then
    region_highlight+=("${#BUFFER} $(( $#BUFFER + $#POSTDISPLAY )) ${__syncsh_suggest_hl} memo=syncsh-suggest")
  fi
}

__syncsh_suggest_redraw() {
  __syncsh_suggest_update
}

syncsh-suggest-accept() {
  emulate -L zsh
  local fb="${__syncsh_suggest_fallback[$KEYS]:-forward-char}"
  if [[ -n ${POSTDISPLAY:-} ]]; then
    if [[ $fb == forward-char && $CURSOR -ne $#BUFFER ]]; then
      zle "$fb"
      return
    fi
    LBUFFER+="$POSTDISPLAY"
    unset POSTDISPLAY
    __syncsh_suggest_last="$LBUFFER"
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
`, strings.Join(quoted, " "), bin)
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
