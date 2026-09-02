package shell

import (
	"fmt"
	"strings"
)

type Options struct {
	SuggestEnabled     bool
	SuggestAccept      []string
	SuggestMenu        bool
	SuggestCompletions bool
	SuggestMenuMax     int
	IconTyped          string
	IconHistory        string
	IconCompletion     string
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
		suggest = zshSuggest(opts)
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

func zshSuggest(opts Options) string {
	accept := opts.SuggestAccept
	if len(accept) == 0 {
		accept = []string{"Right"}
	}
	quoted := make([]string, len(accept))
	for i, k := range accept {
		quoted[i] = zshQuote(k)
	}
	out := fmt.Sprintf(`
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
  [[ -n ${__syncsh_in_comp:-} ]] && return
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
	if opts.SuggestMenu {
		out += zshSuggestMenu(opts)
	}
	return out
}

func zshSuggestMenu(opts Options) string {
	typed := opts.IconTyped
	if strings.TrimSpace(typed) == "" {
		typed = "›"
	}
	histIcon := opts.IconHistory
	if strings.TrimSpace(histIcon) == "" {
		histIcon = "*"
	}
	compIcon := opts.IconCompletion
	if strings.TrimSpace(compIcon) == "" {
		compIcon = "+"
	}
	max := opts.SuggestMenuMax
	if max <= 0 {
		max = 8
	}
	if max > 32 {
		max = 32
	}
	out := fmt.Sprintf(`
# LSP-style suggestion menu (dropdown under the line via POSTDISPLAY)
typeset -ga __syncsh_suggest_items
typeset -ga __syncsh_suggest_kinds
typeset -ga __syncsh_suggest_descrs
typeset -ga __syncsh_suggest_lines
typeset -gi __syncsh_suggest_idx=0
typeset -gi __syncsh_suggest_off=2
typeset -gi __syncsh_suggest_view=0
typeset -gi __syncsh_suggest_bar=0
typeset -ga __syncsh_rpc_items
typeset -g __syncsh_suggest_suffix=""
typeset -g __syncsh_suggest_typed=""
typeset -g __syncsh_suggest_ghost=""
typeset -gi __syncsh_suggest_inner=0
typeset -gi __syncsh_suggest_desc_col=0
typeset -ga __syncsh_suggest_hist
typeset -gi __syncsh_suggest_menu_max=%d
typeset -g __syncsh_suggest_icon_typed=%s
typeset -g __syncsh_suggest_icon_history=%s
typeset -g __syncsh_suggest_icon_completion=%s
`, max, zshQuote(typed), zshQuote(histIcon), zshQuote(compIcon))
	out += zshSuggestMenuBody()
	if opts.SuggestCompletions {
		out += zshSuggestCompletions()
	}
	return out
}

func zshSuggestCompletions() string {
	return `
typeset -ga __syncsh_comp_values
typeset -ga __syncsh_comp_descrs
typeset -g __syncsh_comp_cache_key=""
typeset -ga __syncsh_comp_cache_values
typeset -ga __syncsh_comp_cache_descrs

__syncsh_compadd() {
  if ! (( ${+__syncsh_comp_ctx} )); then
    typeset -g __syncsh_comp_ctx=1
    typeset -g __syncsh_comp_prefix="$PREFIX"
    typeset -g __syncsh_comp_suffix="$SUFFIX"
    typeset -g __syncsh_comp_lbuffer="$LBUFFER"
    typeset -g __syncsh_comp_rbuffer="$RBUFFER"
  fi
  (( $#__syncsh_comp_values >= 512 )) && return
  local -a matches descrs
  integer i
  # Let builtin compadd parse flags; -O keeps each match as its own array
  # element. Quoted argv slices join on IFS and cram every word into one row.
  builtin compadd -O matches "$@" 2>/dev/null
  i=1
  while (( i <= $# )); do
    case "${argv[i]}" in
      -d|-ld)
        (( i++ ))
        [[ -n ${argv[i]:-} ]] && descrs=("${(P)argv[i]}")
        ;;
      --) break ;;
    esac
    (( i++ ))
  done
  for (( i=1; i<=$#matches; i++ )); do
    [[ -n ${matches[i]} ]] || continue
    (( $#__syncsh_comp_values >= 512 )) && break
    __syncsh_comp_values+=("${matches[i]}")
    __syncsh_comp_descrs+=("${descrs[i]:-}")
  done
}

__syncsh_comp_list_fn() {
  __syncsh_comp_values=()
  __syncsh_comp_descrs=()
  unset __syncsh_comp_ctx __syncsh_comp_prefix __syncsh_comp_suffix __syncsh_comp_lbuffer __syncsh_comp_rbuffer
  integer had_compadd=0
  if (( ${+functions[compadd]} )); then
    had_compadd=1
    functions -c compadd __syncsh_compadd_prev
  fi
  function compadd { __syncsh_compadd "$@" }
  {
    autoload -Uz _main_complete 2>/dev/null
    (( ${+functions[_main_complete]} )) && _main_complete
  } always {
    unfunction compadd 2>/dev/null
    if (( had_compadd )); then
      functions -c __syncsh_compadd_prev compadd
      unfunction __syncsh_compadd_prev 2>/dev/null
    fi
  }
}

zle -C __syncsh_comp_list list-choices __syncsh_comp_list_fn

__syncsh_suggest_completions() {
  emulate -L zsh
  __syncsh_comp_values=()
  __syncsh_comp_descrs=()
  unset __syncsh_comp_lbuffer __syncsh_comp_rbuffer __syncsh_comp_prefix __syncsh_comp_suffix __syncsh_comp_ctx
  [[ -n ${__syncsh_in_comp:-} ]] && return
  (( ${+_comps} )) || return
  __syncsh_in_comp=1
  zle __syncsh_comp_list >/dev/null 2>&1 || true
  unset __syncsh_in_comp
}

__syncsh_comp_cache_store() {
  emulate -L zsh
  __syncsh_comp_cache_key="$BUFFER"
  __syncsh_comp_cache_values=("${__syncsh_comp_values[@]}")
  __syncsh_comp_cache_descrs=("${__syncsh_comp_descrs[@]}")
}

__syncsh_comp_cache_clear() {
  __syncsh_comp_cache_key=""
  __syncsh_comp_cache_values=()
  __syncsh_comp_cache_descrs=()
  unset __syncsh_comp_lbuffer __syncsh_comp_rbuffer __syncsh_comp_prefix __syncsh_comp_suffix __syncsh_comp_ctx
}

syncsh-suggest-complete() {
  emulate -L zsh
  if (( __syncsh_suggest_idx > 0 )); then
    syncsh-suggest-next
    return
  fi
  if [[ ${__syncsh_comp_cache_key:-} == "$BUFFER" && ${#__syncsh_comp_cache_values} -gt 0 ]]; then
    syncsh-suggest-next
    return
  fi
  __syncsh_suggest_completions
  if (( ${#__syncsh_comp_values} == 0 )); then
    zle expand-or-complete
    return
  fi
  __syncsh_comp_cache_store
  __syncsh_suggest_last="$BUFFER"
  __syncsh_suggest_typed="$BUFFER"
  __syncsh_suggest_idx=0
  __syncsh_suggest_off=2
  __syncsh_suggest_fetch_hist
  __syncsh_suggest_rebuild
  zle redisplay
}

zle -N syncsh-suggest-complete
bindkey $'\t' syncsh-suggest-complete
bindkey -M viins $'\t' syncsh-suggest-complete
`
}

func zshSuggestMenuBody() string {
	return `
# Match internal/tui Catppuccin: accent #F5C2E7, text #CDD6F4, muted #585B70,
# rule/surface #313244, base #1E1E2E.
typeset -g __syncsh_menu_hl_border='fg=#585B70,bg=#1E1E2E'
typeset -g __syncsh_menu_hl_row='fg=#CDD6F4,bg=#1E1E2E'
typeset -g __syncsh_menu_hl_sel='fg=#F5C2E7,bold,bg=#313244'
typeset -g __syncsh_menu_hl_desc='fg=#585B70,bg=#1E1E2E'

__syncsh_rpc_list() {
  emulate -L zsh
  __syncsh_agent_ensure || return 1
  local op=$1 f out in n
  shift
  out=${__syncsh_out:-$__syncsh_fd}
  in=${__syncsh_in:-$__syncsh_fd}
  print -n -u $out -- "$op"$'\0' || { __syncsh_agent_reset; return 1 }
  for f in "$@"; do
    print -n -u $out -- "$f"$'\0' || { __syncsh_agent_reset; return 1 }
  done
  local st
  IFS= read -r -d $'\0' -u $in st || { __syncsh_agent_reset; return 1 }
  IFS= read -r -d $'\0' -u $in n || { __syncsh_agent_reset; return 1 }
  if [[ $st != ok || $n != [0-9]## ]]; then
    __syncsh_agent_reset
    return 1
  fi
  (( n > 64 )) && n=64
  __syncsh_rpc_items=()
  local -i i
  for (( i=1; i<=n; i++ )); do
    IFS= read -r -d $'\0' -u $in f || { __syncsh_agent_reset; return 1 }
    __syncsh_rpc_items+=("$f")
  done
  return 0
}

__syncsh_suggest_highlight() {
  region_highlight=(${region_highlight:#*memo=syncsh-suggest*})
  local suf="${__syncsh_suggest_suffix:-}"
  local -i b=${#BUFFER} p w i n dcol off view bar
  p=b
  if [[ -n $suf ]]; then
    region_highlight+=("$p $(( p + $#suf )) ${__syncsh_suggest_hl} memo=syncsh-suggest")
    p+=$#suf
  fi
  n=${#__syncsh_suggest_items}
  (( n < 2 || __syncsh_suggest_inner < 1 )) && return
  w=$(( __syncsh_suggest_inner + 2 ))
  dcol=${__syncsh_suggest_desc_col:-0}
  off=${__syncsh_suggest_off:-2}
  view=${__syncsh_suggest_view:-0}
  bar=${__syncsh_suggest_bar:-0}
  (( off < 2 )) && off=2
  (( view < 1 )) && view=$(( n - 1 ))
  p+=1
  region_highlight+=("$p $(( p + w )) ${__syncsh_menu_hl_border} memo=syncsh-suggest")
  p+=$(( w + 1 ))
  if (( __syncsh_suggest_idx == 1 )); then
    region_highlight+=("$p $(( p + w )) ${__syncsh_menu_hl_sel} memo=syncsh-suggest")
  else
    region_highlight+=("$p $(( p + w )) ${__syncsh_menu_hl_row} memo=syncsh-suggest")
  fi
  p+=$(( w + 1 ))
  for (( i=off; i<off+view && i<=n; i++ )); do
    if (( __syncsh_suggest_idx > 0 && i == __syncsh_suggest_idx )); then
      region_highlight+=("$p $(( p + w )) ${__syncsh_menu_hl_sel} memo=syncsh-suggest")
    else
      region_highlight+=("$p $(( p + w )) ${__syncsh_menu_hl_row} memo=syncsh-suggest")
    fi
    if (( dcol > 0 )) && [[ -n ${__syncsh_suggest_descrs[i]:-} ]]; then
      region_highlight+=("$(( p + 1 + dcol )) $(( p + 1 + __syncsh_suggest_inner )) ${__syncsh_menu_hl_desc} memo=syncsh-suggest")
    fi
    if (( bar )); then
      region_highlight+=("$(( p + w - 1 )) $(( p + w )) ${__syncsh_menu_hl_desc} memo=syncsh-suggest")
    fi
    p+=$(( w + 1 ))
  done
  region_highlight+=("$p $(( p + w )) ${__syncsh_menu_hl_border} memo=syncsh-suggest")
}

__syncsh_suggest_clear() {
  unset POSTDISPLAY __syncsh_suggest_suffix __syncsh_suggest_typed __syncsh_suggest_ghost
  __syncsh_suggest_items=()
  __syncsh_suggest_kinds=()
  __syncsh_suggest_descrs=()
  __syncsh_suggest_lines=()
  __syncsh_suggest_hist=()
  __syncsh_suggest_idx=0
  __syncsh_suggest_off=2
  __syncsh_suggest_view=0
  __syncsh_suggest_bar=0
  (( ${+functions[__syncsh_comp_cache_clear]} )) && __syncsh_comp_cache_clear
  __syncsh_suggest_highlight
}

__syncsh_suggest_scroll() {
  emulate -L zsh
  local -i n=${#__syncsh_suggest_items}
  local -i max=$__syncsh_suggest_menu_max
  local -i nsug=$(( n - 1 ))
  local -i idx=$__syncsh_suggest_idx
  (( max < 1 )) && max=1
  __syncsh_suggest_off=2
  (( nsug < 1 )) && return
  if (( nsug <= max )); then
    return
  fi
  if (( idx <= 1 )); then
    return
  fi
  if (( idx < __syncsh_suggest_off )); then
    __syncsh_suggest_off=$idx
  elif (( idx > __syncsh_suggest_off + max - 1 )); then
    __syncsh_suggest_off=$(( idx - max + 1 ))
  fi
  local -i maxoff=$(( n - max + 1 ))
  (( __syncsh_suggest_off < 2 )) && __syncsh_suggest_off=2
  (( __syncsh_suggest_off > maxoff )) && __syncsh_suggest_off=$maxoff
}

__syncsh_suggest_kind_icon() {
  case "$1" in
    typed) REPLY="${__syncsh_suggest_icon_typed}" ;;
    history) REPLY="${__syncsh_suggest_icon_history}" ;;
    *) REPLY="${__syncsh_suggest_icon_completion}" ;;
  esac
}

__syncsh_suggest_fmt_row() {
  emulate -L zsh
  local -i i=$1 valw=$2 descw=$3 iconw=$4
  local s kind icon marker descr
  kind="${__syncsh_suggest_kinds[i]:-history}"
  __syncsh_suggest_kind_icon "$kind"
  icon="$REPLY"
  icon="${(r:iconw:)icon}"
  if (( __syncsh_suggest_idx > 0 && i == __syncsh_suggest_idx )); then
    marker='❯ '
  else
    marker='  '
  fi
  s="${__syncsh_suggest_items[i]}"
  if (( $#s > valw )); then
    s="${s[1,$(( valw - 1 ))]}..."
  fi
  s="${(r:valw:)s}"
  descr="${__syncsh_suggest_descrs[i]:-}"
  if (( descw > 0 )) && [[ -n $descr ]]; then
    if (( $#descr > descw )); then
      descr="${descr[1,$(( descw - 1 ))]}..."
    fi
    s="${marker}${icon} ${s}  ${descr}"
  else
    s="${marker}${icon} ${s}"
  fi
  s="${(r:__syncsh_suggest_inner:)s}"
  REPLY=$s
}

__syncsh_suggest_menu_box() {
  emulate -L zsh
  local -i i n maxv maxd inner cols iconw valw descw markerw=2 overflow off view nsug maxr bar t0 thumb track span rel vis
  local s kind pad line="" fill="" descr rb
  n=${#__syncsh_suggest_items}
  maxr=$__syncsh_suggest_menu_max
  (( maxr < 1 )) && maxr=1
  __syncsh_suggest_scroll
  off=${__syncsh_suggest_off:-2}
  (( off < 2 )) && off=2
  nsug=$(( n - 1 ))
  view=$nsug
  (( view > maxr )) && view=$maxr
  __syncsh_suggest_view=$view
  bar=0
  (( nsug > maxr )) && bar=1
  __syncsh_suggest_bar=$bar
  iconw=1
  (( $#__syncsh_suggest_icon_typed > iconw )) && iconw=$#__syncsh_suggest_icon_typed
  (( $#__syncsh_suggest_icon_history > iconw )) && iconw=$#__syncsh_suggest_icon_history
  (( $#__syncsh_suggest_icon_completion > iconw )) && iconw=$#__syncsh_suggest_icon_completion
  maxv=4
  maxd=0
  s="${__syncsh_suggest_items[1]}"
  (( $#s > maxv )) && maxv=$#s
  for (( i=off; i<off+view && i<=n; i++ )); do
    s="${__syncsh_suggest_items[i]}"
    (( $#s > maxv )) && maxv=$#s
    descr="${__syncsh_suggest_descrs[i]:-}"
    (( $#descr > maxd )) && maxd=$#descr
  done
  cols=${COLUMNS:-80}
  valw=$maxv
  descw=$maxd
  inner=$(( markerw + iconw + 1 + valw ))
  (( descw > 0 )) && inner=$(( inner + 2 + descw ))
  if (( inner > cols - 4 )); then
    overflow=$(( inner - (cols - 4) ))
    if (( descw > overflow )); then
      descw=$(( descw - overflow ))
    else
      overflow=$(( overflow - descw ))
      descw=0
      valw=$(( valw - overflow ))
      (( valw < 4 )) && valw=4
    fi
    inner=$(( markerw + iconw + 1 + valw ))
    (( descw > 0 )) && inner=$(( inner + 2 + descw ))
  fi
  (( inner < 24 )) && inner=24
  (( inner < 4 )) && inner=4
  __syncsh_suggest_inner=$inner
  __syncsh_suggest_desc_col=0
  (( descw > 0 )) && __syncsh_suggest_desc_col=$(( markerw + iconw + 1 + valw + 2 ))
  thumb=1
  t0=0
  if (( bar )); then
    track=$view
    thumb=$(( track * track / nsug ))
    (( thumb < 1 )) && thumb=1
    (( thumb > track )) && thumb=$track
    span=$(( nsug - track ))
    rel=$(( off - 2 ))
    if (( span > 0 )); then
      t0=$(( rel * (track - thumb) / span ))
    fi
  fi
  pad="${(l:inner::─:)fill}"
  line+="┌${pad}┐"$'\n'
  __syncsh_suggest_fmt_row 1 $valw $descw $iconw
  line+="│$REPLY│"$'\n'
  vis=0
  for (( i=off; i<off+view && i<=n; i++ )); do
    __syncsh_suggest_fmt_row $i $valw $descw $iconw
    rb='│'
    if (( bar )); then
      if (( vis >= t0 && vis < t0 + thumb )); then
        rb='█'
      else
        rb='░'
      fi
    fi
    line+="│$REPLY$rb"$'\n'
    (( vis++ ))
  done
  line+="└${pad}┘"
  REPLY=$line
}

__syncsh_suggest_apply() {
  emulate -L zsh
  __syncsh_suggest_suffix=""
  if [[ -n ${__syncsh_suggest_ghost:-} && $BUFFER == "$__syncsh_suggest_typed" && ${__syncsh_suggest_ghost} == "$BUFFER"* && ${__syncsh_suggest_ghost} != "$BUFFER" ]]; then
    __syncsh_suggest_suffix="${__syncsh_suggest_ghost#"$BUFFER"}"
  fi
  if (( ${#__syncsh_suggest_items} >= 2 )); then
    __syncsh_suggest_menu_box
    POSTDISPLAY="${__syncsh_suggest_suffix}"$'\n'"$REPLY"
  elif [[ -n ${__syncsh_suggest_suffix} ]]; then
    POSTDISPLAY="${__syncsh_suggest_suffix}"
  else
    unset POSTDISPLAY
  fi
  __syncsh_suggest_highlight
}

__syncsh_suggest_fetch_hist() {
  emulate -L zsh
  local -a hist filtered
  local s line
  hist=()
  if __syncsh_rpc_list suggest-list "$BUFFER" "$PWD"; then
    hist=("${__syncsh_rpc_items[@]}")
  else
    while IFS= read -r line; do
      [[ -n $line ]] && hist+=("$line")
    done < <("$__syncsh_bin" suggest --prefix "$BUFFER" --cwd "$PWD" --list 2>/dev/null)
  fi
  filtered=()
  for s in "${hist[@]}"; do
    if [[ $s == "$BUFFER"* && $s != "$BUFFER" ]]; then
      filtered+=("$s")
    fi
  done
  __syncsh_suggest_hist=("${filtered[@]}")
}

__syncsh_suggest_rebuild() {
  emulate -L zsh
  local -a hist cvals cdescrs
  local typed s m d hs applied
  local -i i dup
  hist=("${__syncsh_suggest_hist[@]}")
  cvals=()
  cdescrs=()
  if [[ ${__syncsh_comp_cache_key:-} == "$__syncsh_suggest_typed" ]]; then
    cvals=("${__syncsh_comp_cache_values[@]}")
    cdescrs=("${__syncsh_comp_cache_descrs[@]}")
  fi
  __syncsh_suggest_items=()
  __syncsh_suggest_kinds=()
  __syncsh_suggest_descrs=()
  __syncsh_suggest_lines=()
  __syncsh_suggest_ghost=""
  if (( $#hist == 0 && $#cvals == 0 )); then
    __syncsh_suggest_apply
    return
  fi
  typed="$__syncsh_suggest_typed"
  __syncsh_suggest_items=("$typed")
  __syncsh_suggest_kinds=(typed)
  __syncsh_suggest_descrs=("")
  __syncsh_suggest_lines=("$typed")
  for s in "${hist[@]}"; do
    __syncsh_suggest_items+=("$s")
    __syncsh_suggest_kinds+=(history)
    __syncsh_suggest_descrs+=("")
    __syncsh_suggest_lines+=("$s")
    [[ -z $__syncsh_suggest_ghost ]] && __syncsh_suggest_ghost="$s"
  done
  for (( i=1; i<=$#cvals; i++ )); do
    m="${cvals[i]}"
    [[ -n $m ]] || continue
    if (( ${+__syncsh_comp_lbuffer} )); then
      applied="${__syncsh_comp_lbuffer%$__syncsh_comp_prefix}${m}${__syncsh_comp_rbuffer#$__syncsh_comp_suffix}"
    else
      applied="$m"
    fi
    [[ -n $applied && $applied != "$typed" ]] || continue
    dup=0
    for hs in "${hist[@]}"; do
      if [[ $applied == "$hs" ]]; then
        dup=1
        break
      fi
    done
    (( dup )) && continue
    d="${cdescrs[i]:-}"
    __syncsh_suggest_items+=("$m")
    __syncsh_suggest_kinds+=(completion)
    __syncsh_suggest_descrs+=("$d")
    __syncsh_suggest_lines+=("$applied")
  done
  __syncsh_suggest_scroll
  __syncsh_suggest_apply
}

__syncsh_suggest_update() {
  emulate -L zsh
  [[ -n ${__syncsh_in_comp:-} ]] && return
  [[ $WIDGET == __syncsh_comp_list ]] && return
  if [[ -z $BUFFER ]]; then
    __syncsh_suggest_clear
    __syncsh_suggest_last=""
    return
  fi
  if [[ ${__syncsh_suggest_last} == "$BUFFER" ]]; then
    __syncsh_suggest_apply
    return
  fi
  __syncsh_suggest_last="$BUFFER"
  __syncsh_suggest_typed="$BUFFER"
  __syncsh_suggest_idx=0
  __syncsh_suggest_off=2
  if [[ ${__syncsh_comp_cache_key:-} != "$BUFFER" ]]; then
    (( ${+functions[__syncsh_comp_cache_clear]} )) && __syncsh_comp_cache_clear
  fi
  __syncsh_suggest_fetch_hist
  __syncsh_suggest_rebuild
}

__syncsh_suggest_commit_selection() {
  emulate -L zsh
  local s="${__syncsh_suggest_typed:-}"
  if (( __syncsh_suggest_idx >= 1 && __syncsh_suggest_idx <= ${#__syncsh_suggest_lines} )); then
    s="${__syncsh_suggest_lines[__syncsh_suggest_idx]}"
  fi
  BUFFER="$s"
  CURSOR=$#BUFFER
  __syncsh_suggest_last="$BUFFER"
  __syncsh_suggest_scroll
  __syncsh_suggest_apply
  zle redisplay
}

syncsh-suggest-accept() {
  emulate -L zsh
  local fb="${__syncsh_suggest_fallback[$KEYS]:-forward-char}"
  if [[ -n ${__syncsh_suggest_suffix:-} ]]; then
    if [[ $fb == forward-char && $CURSOR -ne $#BUFFER ]]; then
      zle "$fb"
      return
    fi
    BUFFER="$BUFFER$__syncsh_suggest_suffix"
    unset POSTDISPLAY __syncsh_suggest_suffix
    __syncsh_suggest_items=()
    CURSOR=$#BUFFER
    __syncsh_suggest_last="$BUFFER"
    zle redisplay
    return
  fi
  zle "$fb"
}

syncsh-suggest-next() {
  emulate -L zsh
  local -i n=${#__syncsh_suggest_items}
  if (( n < 2 )); then
    zle down-line-or-history 2>/dev/null || zle .down-line-or-history
    return
  fi
  if (( __syncsh_suggest_idx == 0 )); then
    __syncsh_suggest_idx=2
  elif (( __syncsh_suggest_idx >= n )); then
    __syncsh_suggest_idx=1
  else
    (( __syncsh_suggest_idx++ ))
  fi
  __syncsh_suggest_commit_selection
}

syncsh-suggest-prev() {
  emulate -L zsh
  local -i n=${#__syncsh_suggest_items}
  if (( n < 2 )); then
    zle up-line-or-history 2>/dev/null || zle .up-line-or-history
    return
  fi
  if (( __syncsh_suggest_idx == 0 )); then
    __syncsh_suggest_idx=n
  elif (( __syncsh_suggest_idx == 1 )); then
    __syncsh_suggest_idx=0
  else
    (( __syncsh_suggest_idx-- ))
  fi
  __syncsh_suggest_commit_selection
}

syncsh-suggest-dismiss() {
  emulate -L zsh
  if (( ${#__syncsh_suggest_items} == 0 )) && [[ -z ${__syncsh_suggest_suffix:-} ]]; then
    return
  fi
  if [[ -n ${__syncsh_suggest_typed:-} ]]; then
    BUFFER="$__syncsh_suggest_typed"
    CURSOR=$#BUFFER
    __syncsh_suggest_last="$BUFFER"
  fi
  __syncsh_suggest_clear
  zle redisplay
}

zle -N syncsh-suggest-accept
zle -N syncsh-suggest-next
zle -N syncsh-suggest-prev
zle -N syncsh-suggest-dismiss

() {
  local seq
  for seq in "${terminfo[kcud1]:-}" $'\e[B' $'\eOB'; do
    [[ -n $seq ]] || continue
    bindkey "$seq" syncsh-suggest-next
  done
  for seq in "${terminfo[kcuu1]:-}" $'\e[A' $'\eOA'; do
    [[ -n $seq ]] || continue
    bindkey "$seq" syncsh-suggest-prev
  done
  bindkey '^N' syncsh-suggest-next
  bindkey '^P' syncsh-suggest-prev
  bindkey '\e' syncsh-suggest-dismiss
}
`
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
