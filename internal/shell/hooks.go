package shell

import (
	"fmt"
	"strings"

	"github.com/dont-be-evil-company/remnix/internal/ptyproxy"
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
	PtyProxyEnabled    bool
	AttachBin          string
}

func Integration(shellName, binary string, opts Options) (string, error) {
	if binary == "" {
		binary = "remnix"
	}
	var body string
	switch strings.ToLower(shellName) {
	case "zsh":
		body = zsh(binary, opts)
	case "bash":
		body = bash(binary, opts)
	case "fish":
		body = fish(binary, opts)
	case "nu", "nushell":
		body = nu(binary, opts)
	default:
		return "", fmt.Errorf("unsupported shell %q (want zsh, bash, fish, or nu)", shellName)
	}
	if opts.PtyProxyEnabled {
		return ptyproxy.Preamble(binary, shellName) + body, nil
	}
	return body, nil
}

func zsh(bin string, opts Options) string {
	suggest := ""
	if opts.SuggestEnabled {
		suggest = zshSuggest(opts)
	}
	attach := opts.AttachBin
	if attach == "" {
		attach = "remnix-attach"
	}
	return fmt.Sprintf(`# remnix zsh integration
# Add to ~/.zshrc: eval "$(%s init zsh)"

typeset -g __remnix_bin=%s
typeset -g __remnix_attach=%s
typeset -g __remnix_session="${__remnix_session:-$$-$(date +%%s)}"
typeset -g __remnix_fd=""
typeset -g __remnix_out=""
typeset -g __remnix_in=""

# Atuin widget protocol (atuin.zsh __atuin_search_cmd): TUI on stdout, selected
# command on stderr. Swap those fds under $(...) so the TUI hits the TTY and
# REPLY captures the command. Closing fd 3 avoids leaking the capture pipe.
__remnix_widget_run() {
  emulate -L zsh
  local st
  REPLY=$("$__remnix_bin" "$@" 3>&1 1>&2 2>&3 3>&-)
  st=$?
  return $st
}

# Daemon overlay when the shell is inside remnix-attach (REMNIX_SESSION_ID).
# Falls through so the caller can spawn the local Go TUI.
__remnix_overlay() {
  emulate -L zsh
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

__remnix_sock() {
  if [[ -n ${REMNIX_RUNTIME_DIR:-} ]]; then
    print -r -- "$REMNIX_RUNTIME_DIR/control.sock"
  elif [[ -n ${XDG_RUNTIME_DIR:-} ]]; then
    print -r -- "$XDG_RUNTIME_DIR/remnix/control.sock"
  else
    print -r -- "${TMPDIR:-/tmp}/remnix/control.sock"
  fi
}

__remnix_agent_reset() {
  unset __remnix_fd __remnix_out __remnix_in
  __remnix_fd=""
  __remnix_out=""
  __remnix_in=""
}

__remnix_agent_connect() {
  emulate -L zsh
  [[ -n ${__remnix_fd:-} || -n ${__remnix_out:-} ]] && return 0
  if [[ -n ${REMNIX_CONTROL_FD:-} ]]; then
    __remnix_fd=$REMNIX_CONTROL_FD
    return 0
  fi
  local sock
  sock="$(__remnix_sock)"
  if zmodload zsh/net/socket 2>/dev/null && [[ -S $sock ]] && zsocket "$sock" 2>/dev/null; then
    __remnix_fd=$REPLY
    return 0
  fi
  return 1
}

__remnix_agent_ensure() {
  emulate -L zsh
  __remnix_agent_connect && return 0
  "$__remnix_bin" daemon >/dev/null 2>&1 &!
  local i
  for i in {1..20}; do
    __remnix_agent_connect && return 0
    zmodload zsh/zselect 2>/dev/null && zselect -t 1 || sleep 0.01
  done
  return 1
}

__remnix_rpc_write() {
  emulate -L zsh
  __remnix_agent_ensure || return 1
  local f out
  out=${__remnix_out:-$__remnix_fd}
  print -n -u $out -- "$1"$'\0' || { __remnix_agent_reset; return 1 }
  shift
  for f in "$@"; do
    print -n -u $out -- "$f"$'\0' || { __remnix_agent_reset; return 1 }
  done
  return 0
}

__remnix_rpc_read() {
  emulate -L zsh
  local in st
  in=${__remnix_in:-$__remnix_fd}
  IFS= read -r -d $'\0' -u $in st || { __remnix_agent_reset; return 1 }
  IFS= read -r -d $'\0' -u $in REPLY || { __remnix_agent_reset; return 1 }
  [[ $st == ok ]]
}

__remnix_rpc() {
  emulate -L zsh
  __remnix_rpc_write "$@" && __remnix_rpc_read
}

__remnix_preexec() {
  local cmd="${1:-}"
  [[ -z "$cmd" ]] && return
  if __remnix_rpc start "$cmd" "$PWD" "$__remnix_session" zsh; then
    __remnix_id="$REPLY"
    return
  fi
  __remnix_id="$("$__remnix_bin" history start --command "$cmd" --cwd "$PWD" --session "$__remnix_session" --shell zsh 2>/dev/null)" || true
}

__remnix_precmd() {
  local code=$?
  # Drop accept-time suppress after the command finishes so the next line can
  # show ghost text again. Must outlive accept-line: oh-my-posh's
  # zle-line-init runs reset-prompt after recursive-edit returns, and clearing
  # suppress earlier lets the ghost be painted back into the frozen line.
  (( ${+__remnix_suggest_suppress} )) && __remnix_suggest_suppress=0
  if [[ -n "${__remnix_id:-}" ]]; then
    __remnix_rpc end "$__remnix_id" "$code" || "$__remnix_bin" history end --id "$__remnix_id" --exit "$code" >/dev/null 2>&1 || true
    unset __remnix_id
  fi
}

remnix-search() {
  local selected run=0
  # Block suggest redraw/update for the whole widget and until precmd.
  # oh-my-posh runs reset-prompt after recursive-edit returns; clearing
  # suppress in an always-block would let the ghost be painted back into the
  # frozen command line (e.g. "jj f" when only "jj" ran).
  __remnix_suggest_suppress=1
  if (( ${+functions[__remnix_suggest_clear]} )); then
    __remnix_suggest_clear
    zle redisplay
  fi
  zle -I
  if ! __remnix_overlay search-interactive "$LBUFFER"; then
    __remnix_widget_run search --interactive --query "$LBUFFER" --cwd "$PWD" || {
      __remnix_suggest_suppress=0
      return
    }
  fi
  selected="$REPLY"
  if [[ "$selected" == __remnix_accept__:* ]]; then
    selected="${selected#__remnix_accept__:}"
    run=1
  fi
  if [[ -n "$selected" ]]; then
    LBUFFER="$selected"
    RBUFFER=""
  fi
  if (( ${+functions[__remnix_suggest_clear]} )); then
    __remnix_suggest_clear
    # Redisplay first (known cursor), then clear below - reverse order misses
    # rows restored by alt-screen.
    zle redisplay
    [[ -n ${terminfo[ed]:-} ]] && echoti ed
  fi
  zle reset-prompt
  # Use the builtin. Nested "zle accept-line" runs the suggest wrapper with
  # WIDGET still set to remnix-search, so orig lookup is empty → "No such widget".
  if (( run )); then
    zle .accept-line
    # suppress stays 1 until __remnix_precmd
  else
    __remnix_suggest_suppress=0
  fi
}

zle -N remnix-search
bindkey '^R' remnix-search
bindkey -M viins '^R' remnix-search

autoload -Uz add-zsh-hook
add-zsh-hook preexec __remnix_preexec
add-zsh-hook precmd __remnix_precmd
__remnix_agent_ensure >/dev/null 2>&1 || true
%s`, bin, zshQuote(bin), zshQuote(attach), suggest)
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
typeset -ga __remnix_suggest_accept
__remnix_suggest_accept=(%s)
typeset -gA __remnix_suggest_fallback
typeset -g __remnix_suggest_last=""
typeset -gi __remnix_suggest_suppress=0
typeset -gi __remnix_suggest_clearing=0
# zsh 5.9 ignores "faint"; 238 is a muted gray that actually recedes
typeset -g __remnix_suggest_hl=fg=238
zle_highlight=(${zle_highlight:#suffix:*})
zle_highlight+=(suffix:${__remnix_suggest_hl})

# Color POSTDISPLAY with both suffix (zle_highlight) and region_highlight.
# suffix alone is ignored by some zsh/theme combos and the ghost goes white.
# Enter still drops POSTDISPLAY before accept-line, so the region does not
# get committed as part of the command.
__remnix_suggest_highlight() {
  region_highlight=(${region_highlight:#*memo=remnix-suggest*})
  if [[ -n ${POSTDISPLAY:-} ]]; then
    region_highlight+=("${#BUFFER} $(( $#BUFFER + $#POSTDISPLAY )) ${__remnix_suggest_hl} memo=remnix-suggest")
  fi
}

__remnix_suggest_clear() {
  unset POSTDISPLAY
  __remnix_suggest_last=""
  __remnix_suggest_highlight
}

__remnix_suggest_update() {
  emulate -L zsh
  (( __remnix_suggest_suppress )) && return
  # POSTDISPLAY is appended after BUFFER, not the cursor. Matching on
  # LBUFFER makes the ghost overlap RBUFFER as soon as the cursor moves left.
  if [[ -z $BUFFER ]]; then
    unset POSTDISPLAY
    __remnix_suggest_last=""
    __remnix_suggest_highlight
    return
  fi
  if [[ ${__remnix_suggest_last} == "$BUFFER" ]]; then
    __remnix_suggest_highlight
    return
  fi
  __remnix_suggest_last="$BUFFER"
  local s=""
  if __remnix_rpc suggest "$BUFFER" "$PWD"; then
    s="$REPLY"
  else
    s="$("$__remnix_bin" suggest --prefix "$BUFFER" --cwd "$PWD" 2>/dev/null)" || s=""
  fi
  if [[ -n $s && $s == "$BUFFER"* && $s != "$BUFFER" ]]; then
    POSTDISPLAY="${s#"$BUFFER"}"
  else
    unset POSTDISPLAY
  fi
  __remnix_suggest_highlight
}

__remnix_suggest_redraw() {
  [[ -n ${__remnix_in_comp:-} ]] && return
  # Hard stop while ctrl+r / accept-line is tearing down the multi-line menu.
  # Prompt themes (oh-my-posh) re-enter redraw under their own WIDGET names.
  if (( __remnix_suggest_suppress )); then
    __remnix_suggest_clear
    return
  fi
  # "zle redisplay" / "zle reset-prompt" set WIDGET to those names, not the
  # caller. Updating here rebuilds the multi-line menu and can recurse through
  # compsys (FUNCNEST) or leave the box in scrollback on accept-line.
  case $WIDGET in
    accept-line|accept-and-hold|accept-line-and-down-history|accept-and-infer-next-history|remnix-search|redisplay|reset-prompt)
      __remnix_suggest_clear
      return
      ;;
  esac
  __remnix_suggest_update
}

__remnix_suggest_bind_clear() {
  emulate -L zsh
  local w orig
  for w in accept-line accept-and-hold accept-line-and-down-history accept-and-infer-next-history; do
    orig="__remnix_suggest_orig_$w"
    # Capture the underlying widget once; always refresh the wrapper body so
    # re-eval "$(remnix init zsh)" picks up clear fixes.
    if [[ ${widgets[$w]:-} != user:__remnix_suggest_clear_then_$w ]]; then
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
    fi
    # One wrapper per widget. A shared wrapper keyed on $WIDGET breaks when
    # another widget (remnix-search) invokes accept-line: $WIDGET stays the caller.
    # Suppress until precmd (not an always-block): oh-my-posh's zle-line-init
    # does reset-prompt after recursive-edit returns from accept-line. Clearing
    # suppress there rebuilds POSTDISPLAY into the frozen "jj f" line.
    eval "__remnix_suggest_clear_then_$w() {
      emulate -L zsh
      # Assign the global (do not typeset: that would shadow it).
      __remnix_suggest_suppress=1
      __remnix_suggest_clear
      zle -R
      zle $orig
    }"
    zle -N "$w" "__remnix_suggest_clear_then_$w"
  done
}

remnix-suggest-accept() {
  emulate -L zsh
  local fb="${__remnix_suggest_fallback[$KEYS]:-forward-char}"
  if [[ -n ${POSTDISPLAY:-} ]]; then
    if [[ $fb == forward-char && $CURSOR -ne $#BUFFER ]]; then
      zle "$fb"
      return
    fi
    BUFFER="$BUFFER$POSTDISPLAY"
    unset POSTDISPLAY
    CURSOR=$#BUFFER
    __remnix_suggest_last="$BUFFER"
    zle redisplay
    return
  fi
  zle "$fb"
}

zle -N remnix-suggest-accept
zle -N __remnix_suggest_redraw

__remnix_suggest_bind() {
  emulate -L zsh
  local spec seq fb
  for spec in "${__remnix_suggest_accept[@]}"; do
    case "${(L)spec}" in
      right|arrow-right|rightarrow|forward-char)
        fb=forward-char
        for seq in "${terminfo[kcuf1]:-}" $'\e[C' $'\eOC'; do
          [[ -n $seq ]] || continue
          __remnix_suggest_fallback[$seq]=$fb
          bindkey "$seq" remnix-suggest-accept
        done
        ;;
      tab)
        fb=expand-or-complete
        __remnix_suggest_fallback[$'\t']=$fb
        bindkey $'\t' remnix-suggest-accept
        ;;
      end)
        fb=end-of-line
        for seq in "${terminfo[kend]:-}" $'\e[F' $'\eOF'; do
          [[ -n $seq ]] || continue
          __remnix_suggest_fallback[$seq]=$fb
          bindkey "$seq" remnix-suggest-accept
        done
        ;;
      c-e|ctrl-e|^e)
        fb=end-of-line
        __remnix_suggest_fallback[$'^E']=$fb
        bindkey '^E' remnix-suggest-accept
        ;;
      c-f|ctrl-f|^f)
        fb=forward-char
        __remnix_suggest_fallback[$'^F']=$fb
        bindkey '^F' remnix-suggest-accept
        ;;
      *)
        fb=forward-char
        __remnix_suggest_fallback[$spec]=$fb
        bindkey -- "$spec" remnix-suggest-accept
        ;;
    esac
  done
}

__remnix_suggest_bind
__remnix_suggest_bind_clear

if autoload -Uz add-zle-hook-widget 2>/dev/null; then
  add-zle-hook-widget -d line-pre-redraw __remnix_suggest_redraw 2>/dev/null || true
  add-zle-hook-widget line-pre-redraw __remnix_suggest_redraw 2>/dev/null && :
else
  __remnix_suggest_self_insert() {
    zle .self-insert
    __remnix_suggest_update
    zle -R
  }
  __remnix_suggest_backward_delete() {
    zle .backward-delete-char
    __remnix_suggest_last=""
    __remnix_suggest_update
    zle -R
  }
  zle -N self-insert __remnix_suggest_self_insert
  zle -N backward-delete-char __remnix_suggest_backward_delete
fi
`, strings.Join(quoted, " "))
	if opts.SuggestMenu {
		if opts.PtyProxyEnabled {
			out += zshOverlayMenu()
			out += zshSuggestCompletions(false)
		} else {
			out += zshSuggestMenu(opts)
		}
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
typeset -ga __remnix_suggest_items
typeset -ga __remnix_suggest_kinds
typeset -ga __remnix_suggest_descrs
typeset -ga __remnix_suggest_lines
typeset -gi __remnix_suggest_idx=0
typeset -gi __remnix_suggest_off=2
typeset -gi __remnix_suggest_view=0
typeset -gi __remnix_suggest_bar=0
typeset -g __remnix_suggest_suffix=""
typeset -g __remnix_suggest_typed=""
typeset -g __remnix_suggest_ghost=""
typeset -gi __remnix_suggest_inner=0
typeset -gi __remnix_suggest_desc_col=0
typeset -gi __remnix_suggest_menu_max=%d
typeset -g __remnix_suggest_icon_typed=%s
typeset -g __remnix_suggest_icon_history=%s
typeset -g __remnix_suggest_icon_completion=%s
`, max, zshQuote(typed), zshQuote(histIcon), zshQuote(compIcon))
	out += zshSuggestMenuBody()
	if opts.SuggestCompletions {
		out += zshSuggestCompletions(true)
	}
	return out
}

func zshSuggestCompletions(bindTab bool) string {
	out := `
typeset -ga __remnix_comp_values
typeset -ga __remnix_comp_inserts
typeset -ga __remnix_comp_descrs
typeset -ga __remnix_comp_lines
typeset -ga __remnix_comp_line_descrs
typeset -g __remnix_comp_cache_key=""
typeset -ga __remnix_comp_cache_values
typeset -ga __remnix_comp_cache_inserts
typeset -ga __remnix_comp_cache_descrs

# Capture compsys matches as they would be inserted. builtin -O expands
# -k/-a and applies PREFIX; -p/-P/-s/-S are restored when rewriting the
# line (e.g. ./ from _path_files). PREFIX/LBUFFER are snapshotted in
# the completion widget before completers run: _path_files rewrites
# PREFIX to the last component, and capturing that then prepending -p
# duplicated ./ (cat ./T + Taskfile.yml → cat ././Taskfile.yml).
__remnix_compadd() {
  if ! (( ${+__remnix_comp_ctx} )); then
    typeset -g __remnix_comp_ctx=1
    typeset -g __remnix_comp_prefix="$PREFIX"
    typeset -g __remnix_comp_suffix="$SUFFIX"
    typeset -g __remnix_comp_lbuffer="$LBUFFER"
    typeset -g __remnix_comp_rbuffer="$RBUFFER"
  fi
  local -a matches descrs src
  local dvar="" avar="" m d insert flags c rest
  local opt_i="" opt_P="" opt_p="" opt_s="" opt_S="" opt_I=""
  integer i probe=0 j fidx si
  i=1
  while (( i <= $# )); do
    case "${argv[i]}" in
      --) break ;;
      -*)
        flags="${argv[i]#-}"
        fidx=1
        while (( fidx <= $#flags )); do
          c="${flags[fidx]}"
          rest="${flags[fidx+1,-1]}"
          case "$c" in
            O|A|D)
              probe=1
              if [[ -z $rest ]]; then
                (( i++ ))
              fi
              fidx=$#flags
              ;;
            P)
              if [[ -n $rest ]]; then
                opt_P="$rest"
              else
                (( i++ ))
                opt_P="${argv[i]}"
              fi
              fidx=$#flags
              ;;
            p)
              if [[ -n $rest ]]; then
                opt_p="$rest"
              else
                (( i++ ))
                opt_p="${argv[i]}"
              fi
              fidx=$#flags
              ;;
            S)
              if [[ -n $rest ]]; then
                opt_S="$rest"
              else
                (( i++ ))
                opt_S="${argv[i]}"
              fi
              fidx=$#flags
              ;;
            s)
              if [[ -n $rest ]]; then
                opt_s="$rest"
              else
                (( i++ ))
                opt_s="${argv[i]}"
              fi
              fidx=$#flags
              ;;
            i)
              if [[ -n $rest ]]; then
                opt_i="$rest"
              else
                (( i++ ))
                opt_i="${argv[i]}"
              fi
              fidx=$#flags
              ;;
            I)
              if [[ -n $rest ]]; then
                opt_I="$rest"
              else
                (( i++ ))
                opt_I="${argv[i]}"
              fi
              fidx=$#flags
              ;;
            d)
              if [[ -n $rest ]]; then
                dvar="$rest"
              else
                (( i++ ))
                dvar="${argv[i]}"
              fi
              fidx=$#flags
              ;;
            a)
              if [[ -n $rest ]]; then
                avar="$rest"
              else
                (( i++ ))
                avar="${argv[i]}"
              fi
              fidx=$#flags
              ;;
            k|o|J|V|X|x|W|F|M|E|r|R)
              if [[ -z $rest ]]; then
                (( i++ ))
              fi
              fidx=$#flags
              ;;
            *)
              (( fidx++ ))
              continue
              ;;
          esac
          (( fidx++ ))
        done
        ;;
      *) break ;;
    esac
    (( i++ ))
  done
  # _describe probes with -O/-A/-D; those must reach the builtin so later
  # -d/-a calls get real per-match descriptions.
  if (( probe )); then
    builtin compadd "$@"
    return
  fi
  (( $#__remnix_comp_values >= 512 )) && return
  builtin compadd -O matches "$@" 2>/dev/null
  if [[ -n $dvar ]]; then
    descrs=("${(@P)dvar}")
  fi
  if [[ -n $avar ]]; then
    src=("${(@P)avar}")
  fi
  for (( i=1; i<=$#matches; i++ )); do
    m="${matches[i]}"
    d=""
    [[ -n $m ]] || continue
    if (( $#descrs == $#matches )); then
      d="${descrs[i]:-}"
    elif (( $#src == $#descrs && $#descrs > 0 )); then
      for (( si=1; si<=$#src; si++ )); do
        if [[ ${src[si]} == "$m" || ${src[si]} == "$m":* ]]; then
          d="${descrs[si]:-}"
          break
        fi
      done
    fi
    if [[ -z $d && $m == *:* && $m != *://* ]]; then
      d="${m#*:}"
      m="${m%%:*}"
    fi
    if [[ $d == "$m"[[:space:]]#--[[:space:]]* ]]; then
      d="${d#*"-- "}"
    elif [[ $d == [[:space:]]#--[[:space:]]* ]]; then
      d="${d##[[:space:]]#--[[:space:]]#}"
    fi
    (( $#__remnix_comp_values >= 512 )) && break
    j=0
    for (( j=1; j<=$#__remnix_comp_values; j++ )); do
      if [[ ${__remnix_comp_values[j]} == "$m" ]]; then
        if [[ -n $d && -z ${__remnix_comp_descrs[j]} ]]; then
          __remnix_comp_descrs[j]="$d"
        fi
        j=-1
        break
      fi
    done
    (( j < 0 )) && continue
    insert="${opt_i}${opt_P}${opt_p}${m}${opt_s}${opt_S}${opt_I}"
    __remnix_comp_values+=("$m")
    __remnix_comp_inserts+=("$insert")
    __remnix_comp_descrs+=("$d")
  done
}

__remnix_comp_list_fn() {
  __remnix_comp_values=()
  __remnix_comp_inserts=()
  __remnix_comp_descrs=()
  typeset -g __remnix_comp_ctx=1
  typeset -g __remnix_comp_prefix="$PREFIX"
  typeset -g __remnix_comp_suffix="$SUFFIX"
  typeset -g __remnix_comp_lbuffer="$LBUFFER"
  typeset -g __remnix_comp_rbuffer="$RBUFFER"
  integer had_compadd=0
  if (( ${+functions[compadd]} )); then
    had_compadd=1
    functions -c compadd __remnix_compadd_prev
  fi
  function compadd { __remnix_compadd "$@" }
  {
    autoload -Uz _main_complete 2>/dev/null
    (( ${+functions[_main_complete]} )) && _main_complete
  } always {
    unfunction compadd 2>/dev/null
    if (( had_compadd )); then
      functions -c __remnix_compadd_prev compadd
      unfunction __remnix_compadd_prev 2>/dev/null
    fi
  }
}

zle -C __remnix_comp_list list-choices __remnix_comp_list_fn

__remnix_suggest_completions() {
  emulate -L zsh
  __remnix_comp_values=()
  __remnix_comp_inserts=()
  __remnix_comp_descrs=()
  unset __remnix_comp_lbuffer __remnix_comp_rbuffer __remnix_comp_prefix __remnix_comp_suffix __remnix_comp_ctx
  [[ -n ${__remnix_in_comp:-} ]] && return
  (( ${+_comps} )) || return
  __remnix_in_comp=1
  {
    zle __remnix_comp_list >/dev/null 2>&1 || true
    # Command-only / exact-word buffers often yield no next-token matches
    # until a trailing space starts that command's completer. A sole
    # "gcloud storage " row (typed plus whitespace) used to skip the retry
    # and reopen the same leaf instead of buckets/cp/cat.
    if (( ${+functions[__remnix_comp_applied_lines]} )); then
      __remnix_comp_applied_lines "$BUFFER"
    fi
    local -i need_space=0
    if [[ -n $BUFFER && $BUFFER != *[[:space:]] ]]; then
      need_space=1
      local s rest
      for s in "${__remnix_comp_lines[@]}"; do
        rest="${s#"$BUFFER"}"
        if [[ -n ${rest//[[:space:]]/} ]]; then
          need_space=0
          break
        fi
      done
    fi
    if (( need_space )); then
      local orig="$BUFFER" origc=$CURSOR
      BUFFER="$BUFFER "
      CURSOR=$#BUFFER
      zle __remnix_comp_list >/dev/null 2>&1 || true
      BUFFER="$orig"
      CURSOR=$origc
    fi
  } always {
    unset __remnix_in_comp
  }
}

__remnix_comp_cache_store() {
  emulate -L zsh
  __remnix_comp_cache_key="$BUFFER"
  __remnix_comp_cache_values=("${__remnix_comp_values[@]}")
  __remnix_comp_cache_inserts=("${__remnix_comp_inserts[@]}")
  __remnix_comp_cache_descrs=("${__remnix_comp_descrs[@]}")
}

__remnix_comp_cache_clear() {
  __remnix_comp_cache_key=""
  __remnix_comp_cache_values=()
  __remnix_comp_cache_inserts=()
  __remnix_comp_cache_descrs=()
  unset __remnix_comp_lbuffer __remnix_comp_rbuffer __remnix_comp_prefix __remnix_comp_suffix __remnix_comp_ctx
}

__remnix_comp_applied_lines() {
  emulate -L zsh
  local typed="${1:-$BUFFER}" m insert line
  local -i i
  __remnix_comp_lines=()
  __remnix_comp_line_descrs=()
  for (( i=1; i<=$#__remnix_comp_values; i++ )); do
    m="${__remnix_comp_values[i]}"
    [[ -n $m ]] || continue
    insert="${__remnix_comp_inserts[i]:-$m}"
    if (( ${+__remnix_comp_lbuffer} )); then
      line="${__remnix_comp_lbuffer%$__remnix_comp_prefix}${insert}${__remnix_comp_rbuffer#$__remnix_comp_suffix}"
    else
      line="$insert"
    fi
    [[ -n $line && $line != "$typed" ]] || continue
    __remnix_comp_lines+=("$line")
    __remnix_comp_line_descrs+=("${__remnix_comp_descrs[i]:-}")
  done
}

`
	if !bindTab {
		return out
	}
	out += `
remnix-suggest-complete() {
  emulate -L zsh
  if (( __remnix_suggest_idx > 0 )); then
    remnix-suggest-next
    return
  fi
  if [[ ${__remnix_comp_cache_key:-} == "$BUFFER" && ${#__remnix_comp_cache_values} -gt 0 ]]; then
    remnix-suggest-next
    return
  fi
  __remnix_suggest_completions
  if (( ${#__remnix_comp_values} == 0 )); then
    zle expand-or-complete
    return
  fi
  __remnix_comp_cache_store
  __remnix_suggest_last="$BUFFER"
  __remnix_suggest_typed="$BUFFER"
  # Open the dropdown (idx>0); otherwise apply keeps a single-line ghost only.
  __remnix_suggest_idx=1
  __remnix_suggest_off=2
  __remnix_suggest_fetch_ghost
  __remnix_suggest_rebuild
  zle redisplay
}

zle -N remnix-suggest-complete
bindkey $'\t' remnix-suggest-complete
bindkey -M viins $'\t' remnix-suggest-complete
`
	return out
}

func zshSuggestMenuBody() string {
	return `
# Match internal/tui Catppuccin: accent #F5C2E7, text #CDD6F4, muted #585B70,
# rule/surface #313244, base #1E1E2E.
typeset -g __remnix_menu_hl_border='fg=#585B70,bg=#1E1E2E'
typeset -g __remnix_menu_hl_row='fg=#CDD6F4,bg=#1E1E2E'
typeset -g __remnix_menu_hl_sel='fg=#F5C2E7,bold,bg=#313244'
typeset -g __remnix_menu_hl_desc='fg=#585B70,bg=#1E1E2E'

__remnix_suggest_highlight() {
  region_highlight=(${region_highlight:#*memo=remnix-suggest*})
  local suf="${__remnix_suggest_suffix:-}"
  local -i b=${#BUFFER} p w i n dcol off view bar
  p=b
  if [[ -n $suf ]]; then
    region_highlight+=("$p $(( p + $#suf )) ${__remnix_suggest_hl} memo=remnix-suggest")
    p+=$#suf
  fi
  n=${#__remnix_suggest_items}
  (( n < 2 || __remnix_suggest_inner < 1 )) && return
  w=$(( __remnix_suggest_inner + 2 ))
  dcol=${__remnix_suggest_desc_col:-0}
  off=${__remnix_suggest_off:-2}
  view=${__remnix_suggest_view:-0}
  bar=${__remnix_suggest_bar:-0}
  (( off < 2 )) && off=2
  (( view < 1 )) && view=$(( n - 1 ))
  p+=1
  region_highlight+=("$p $(( p + w )) ${__remnix_menu_hl_border} memo=remnix-suggest")
  p+=$(( w + 1 ))
  if (( __remnix_suggest_idx == 1 )); then
    region_highlight+=("$p $(( p + w )) ${__remnix_menu_hl_sel} memo=remnix-suggest")
  else
    region_highlight+=("$p $(( p + w )) ${__remnix_menu_hl_row} memo=remnix-suggest")
  fi
  p+=$(( w + 1 ))
  for (( i=off; i<off+view && i<=n; i++ )); do
    if (( __remnix_suggest_idx > 0 && i == __remnix_suggest_idx )); then
      region_highlight+=("$p $(( p + w )) ${__remnix_menu_hl_sel} memo=remnix-suggest")
    else
      region_highlight+=("$p $(( p + w )) ${__remnix_menu_hl_row} memo=remnix-suggest")
    fi
    if (( dcol > 0 )) && [[ -n ${__remnix_suggest_descrs[i]:-} ]]; then
      region_highlight+=("$(( p + 1 + dcol )) $(( p + 1 + __remnix_suggest_inner )) ${__remnix_menu_hl_desc} memo=remnix-suggest")
    fi
    if (( bar )); then
      region_highlight+=("$(( p + w - 1 )) $(( p + w )) ${__remnix_menu_hl_desc} memo=remnix-suggest")
    fi
    p+=$(( w + 1 ))
  done
  region_highlight+=("$p $(( p + w )) ${__remnix_menu_hl_border} memo=remnix-suggest")
}

__remnix_suggest_clear() {
  # Re-entrancy guard: line-pre-redraw can nest into clear while we unset state.
  (( ${+__remnix_suggest_clearing} )) && (( __remnix_suggest_clearing )) && return
  __remnix_suggest_clearing=1
  unset POSTDISPLAY __remnix_suggest_suffix __remnix_suggest_typed __remnix_suggest_ghost
  __remnix_suggest_last=""
  __remnix_suggest_items=()
  __remnix_suggest_kinds=()
  __remnix_suggest_descrs=()
  __remnix_suggest_lines=()
  __remnix_suggest_idx=0
  __remnix_suggest_off=2
  __remnix_suggest_view=0
  __remnix_suggest_bar=0
  __remnix_comp_cache_key=""
  __remnix_comp_cache_values=()
  __remnix_comp_cache_inserts=()
  __remnix_comp_cache_descrs=()
  unset __remnix_comp_lbuffer __remnix_comp_rbuffer __remnix_comp_prefix __remnix_comp_suffix __remnix_comp_ctx
  __remnix_suggest_highlight
  __remnix_suggest_clearing=0
}

__remnix_suggest_scroll() {
  emulate -L zsh
  local -i n=${#__remnix_suggest_items}
  local -i max=$__remnix_suggest_menu_max
  local -i nsug=$(( n - 1 ))
  local -i idx=$__remnix_suggest_idx
  (( max < 1 )) && max=1
  __remnix_suggest_off=2
  (( nsug < 1 )) && return
  if (( nsug <= max )); then
    return
  fi
  if (( idx <= 1 )); then
    return
  fi
  if (( idx < __remnix_suggest_off )); then
    __remnix_suggest_off=$idx
  elif (( idx > __remnix_suggest_off + max - 1 )); then
    __remnix_suggest_off=$(( idx - max + 1 ))
  fi
  local -i maxoff=$(( n - max + 1 ))
  (( __remnix_suggest_off < 2 )) && __remnix_suggest_off=2
  (( __remnix_suggest_off > maxoff )) && __remnix_suggest_off=$maxoff
}

__remnix_suggest_kind_icon() {
  case "$1" in
    typed) REPLY="${__remnix_suggest_icon_typed}" ;;
    history) REPLY="${__remnix_suggest_icon_history}" ;;
    *) REPLY="${__remnix_suggest_icon_completion}" ;;
  esac
}

__remnix_suggest_fmt_row() {
  emulate -L zsh
  local -i i=$1 valw=$2 descw=$3 iconw=$4
  local s kind icon marker descr
  kind="${__remnix_suggest_kinds[i]:-completion}"
  __remnix_suggest_kind_icon "$kind"
  icon="$REPLY"
  icon="${(r:iconw:)icon}"
  if (( __remnix_suggest_idx > 0 && i == __remnix_suggest_idx )); then
    marker='❯ '
  else
    marker='  '
  fi
  s="${__remnix_suggest_items[i]}"
  if (( $#s > valw )); then
    s="${s[1,$(( valw - 1 ))]}..."
  fi
  s="${(r:valw:)s}"
  descr="${__remnix_suggest_descrs[i]:-}"
  if (( descw > 0 )) && [[ -n $descr ]]; then
    if (( $#descr > descw )); then
      descr="${descr[1,$(( descw - 1 ))]}..."
    fi
    s="${marker}${icon} ${s}  ${descr}"
  else
    s="${marker}${icon} ${s}"
  fi
  s="${(r:__remnix_suggest_inner:)s}"
  REPLY=$s
}

__remnix_suggest_menu_box() {
  emulate -L zsh
  local -i i n maxv maxd inner cols iconw valw descw markerw=2 overflow off view nsug maxr bar t0 thumb track span rel vis
  local s kind pad line="" fill="" descr rb
  n=${#__remnix_suggest_items}
  maxr=$__remnix_suggest_menu_max
  (( maxr < 1 )) && maxr=1
  __remnix_suggest_scroll
  off=${__remnix_suggest_off:-2}
  (( off < 2 )) && off=2
  nsug=$(( n - 1 ))
  view=$nsug
  (( view > maxr )) && view=$maxr
  __remnix_suggest_view=$view
  bar=0
  (( nsug > maxr )) && bar=1
  __remnix_suggest_bar=$bar
  iconw=1
  (( $#__remnix_suggest_icon_typed > iconw )) && iconw=$#__remnix_suggest_icon_typed
  (( $#__remnix_suggest_icon_history > iconw )) && iconw=$#__remnix_suggest_icon_history
  (( $#__remnix_suggest_icon_completion > iconw )) && iconw=$#__remnix_suggest_icon_completion
  maxv=4
  maxd=0
  s="${__remnix_suggest_items[1]}"
  (( $#s > maxv )) && maxv=$#s
  for (( i=off; i<off+view && i<=n; i++ )); do
    s="${__remnix_suggest_items[i]}"
    (( $#s > maxv )) && maxv=$#s
    descr="${__remnix_suggest_descrs[i]:-}"
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
  __remnix_suggest_inner=$inner
  __remnix_suggest_desc_col=0
  (( descw > 0 )) && __remnix_suggest_desc_col=$(( markerw + iconw + 1 + valw + 2 ))
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
  __remnix_suggest_fmt_row 1 $valw $descw $iconw
  line+="│$REPLY│"$'\n'
  vis=0
  for (( i=off; i<off+view && i<=n; i++ )); do
    __remnix_suggest_fmt_row $i $valw $descw $iconw
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

__remnix_suggest_apply() {
  emulate -L zsh
  __remnix_suggest_suffix=""
  if [[ -n ${__remnix_suggest_ghost:-} && $BUFFER == "$__remnix_suggest_typed" && ${__remnix_suggest_ghost} == "$BUFFER"* && ${__remnix_suggest_ghost} != "$BUFFER" ]]; then
    __remnix_suggest_suffix="${__remnix_suggest_ghost#"$BUFFER"}"
  fi
  # Multi-line POSTDISPLAY cannot be cleared reliably on accept-line (zsh +
  # prompt themes leave the box in scrollback). Only paint the dropdown while
  # the user is navigating it; otherwise keep a single-line ghost suffix.
  if (( ${#__remnix_suggest_items} >= 2 && __remnix_suggest_idx > 0 )); then
    __remnix_suggest_menu_box
    POSTDISPLAY="${__remnix_suggest_suffix}"$'\n'"$REPLY"
  elif [[ -n ${__remnix_suggest_suffix} ]]; then
    POSTDISPLAY="${__remnix_suggest_suffix}"
  else
    unset POSTDISPLAY
  fi
  __remnix_suggest_highlight
}

__remnix_suggest_fetch_ghost() {
  emulate -L zsh
  local s=""
  if __remnix_rpc suggest "$BUFFER" "$PWD"; then
    s="$REPLY"
  else
    s="$("$__remnix_bin" suggest --prefix "$BUFFER" --cwd "$PWD" 2>/dev/null)" || s=""
  fi
  if [[ -n $s && $s == "$BUFFER"* && $s != "$BUFFER" ]]; then
    __remnix_suggest_ghost="$s"
  else
    __remnix_suggest_ghost=""
  fi
}

__remnix_suggest_rebuild() {
  emulate -L zsh
  local -a cvals cdescrs cinserts
  local typed m d applied insert
  local -i i
  cvals=()
  cdescrs=()
  cinserts=()
  if [[ ${__remnix_comp_cache_key:-} == "$__remnix_suggest_typed" ]]; then
    cvals=("${__remnix_comp_cache_values[@]}")
    cdescrs=("${__remnix_comp_cache_descrs[@]}")
    cinserts=("${__remnix_comp_cache_inserts[@]}")
  fi
  __remnix_suggest_items=()
  __remnix_suggest_kinds=()
  __remnix_suggest_descrs=()
  __remnix_suggest_lines=()
  if (( $#cvals == 0 )); then
    __remnix_suggest_apply
    return
  fi
  typed="$__remnix_suggest_typed"
  __remnix_suggest_items=("$typed")
  __remnix_suggest_kinds=(typed)
  __remnix_suggest_descrs=("")
  __remnix_suggest_lines=("$typed")
  for (( i=1; i<=$#cvals; i++ )); do
    m="${cvals[i]}"
    [[ -n $m ]] || continue
    insert="${cinserts[i]:-$m}"
    if (( ${+__remnix_comp_lbuffer} )); then
      applied="${__remnix_comp_lbuffer%$__remnix_comp_prefix}${insert}${__remnix_comp_rbuffer#$__remnix_comp_suffix}"
    else
      applied="$insert"
    fi
    [[ -n $applied && $applied != "$typed" ]] || continue
    d="${cdescrs[i]:-}"
    __remnix_suggest_items+=("$m")
    __remnix_suggest_kinds+=(completion)
    __remnix_suggest_descrs+=("$d")
    __remnix_suggest_lines+=("$applied")
  done
  __remnix_suggest_scroll
  __remnix_suggest_apply
}

__remnix_suggest_update() {
  emulate -L zsh
  (( __remnix_suggest_suppress )) && return
  [[ -n ${__remnix_in_comp:-} ]] && return
  [[ $WIDGET == __remnix_comp_list ]] && return
  if [[ -z $BUFFER ]]; then
    __remnix_suggest_clear
    __remnix_suggest_last=""
    return
  fi
  if [[ ${__remnix_suggest_last} == "$BUFFER" ]]; then
    __remnix_suggest_apply
    return
  fi
  __remnix_suggest_last="$BUFFER"
  __remnix_suggest_typed="$BUFFER"
  __remnix_suggest_idx=0
  __remnix_suggest_off=2
  if [[ ${__remnix_comp_cache_key:-} != "$BUFFER" ]]; then
    (( ${+functions[__remnix_comp_cache_clear]} )) && __remnix_comp_cache_clear
  fi
  __remnix_suggest_fetch_ghost
  __remnix_suggest_rebuild
}

__remnix_suggest_commit_selection() {
  emulate -L zsh
  local s="${__remnix_suggest_typed:-}"
  if (( __remnix_suggest_idx >= 1 && __remnix_suggest_idx <= ${#__remnix_suggest_lines} )); then
    s="${__remnix_suggest_lines[__remnix_suggest_idx]}"
  fi
  BUFFER="$s"
  CURSOR=$#BUFFER
  __remnix_suggest_last="$BUFFER"
  __remnix_suggest_scroll
  __remnix_suggest_apply
  zle redisplay
}

remnix-suggest-accept() {
  emulate -L zsh
  local fb="${__remnix_suggest_fallback[$KEYS]:-forward-char}"
  if [[ -n ${__remnix_suggest_suffix:-} ]]; then
    if [[ $fb == forward-char && $CURSOR -ne $#BUFFER ]]; then
      zle "$fb"
      return
    fi
    BUFFER="$BUFFER$__remnix_suggest_suffix"
    unset POSTDISPLAY __remnix_suggest_suffix
    __remnix_suggest_items=()
    CURSOR=$#BUFFER
    __remnix_suggest_last="$BUFFER"
    zle redisplay
    return
  fi
  zle "$fb"
}

remnix-suggest-next() {
  emulate -L zsh
  local -i n=${#__remnix_suggest_items}
  if (( n < 2 )); then
    zle down-line-or-history 2>/dev/null || zle .down-line-or-history
    return
  fi
  if (( __remnix_suggest_idx == 0 )); then
    __remnix_suggest_idx=2
  elif (( __remnix_suggest_idx >= n )); then
    __remnix_suggest_idx=1
  else
    (( __remnix_suggest_idx++ ))
  fi
  __remnix_suggest_commit_selection
}

remnix-suggest-prev() {
  emulate -L zsh
  local -i n=${#__remnix_suggest_items}
  if (( n < 2 )); then
    zle up-line-or-history 2>/dev/null || zle .up-line-or-history
    return
  fi
  if (( __remnix_suggest_idx == 0 )); then
    __remnix_suggest_idx=n
  elif (( __remnix_suggest_idx == 1 )); then
    __remnix_suggest_idx=0
  else
    (( __remnix_suggest_idx-- ))
  fi
  __remnix_suggest_commit_selection
}

remnix-suggest-dismiss() {
  emulate -L zsh
  if (( ${#__remnix_suggest_items} == 0 )) && [[ -z ${__remnix_suggest_suffix:-} ]]; then
    return
  fi
  if [[ -n ${__remnix_suggest_typed:-} ]]; then
    BUFFER="$__remnix_suggest_typed"
    CURSOR=$#BUFFER
    __remnix_suggest_last="$BUFFER"
  fi
  __remnix_suggest_clear
  zle redisplay
}

zle -N remnix-suggest-accept
zle -N remnix-suggest-next
zle -N remnix-suggest-prev
zle -N remnix-suggest-dismiss

() {
  local seq
  for seq in "${terminfo[kcud1]:-}" $'\e[B' $'\eOB'; do
    [[ -n $seq ]] || continue
    bindkey "$seq" remnix-suggest-next
  done
  for seq in "${terminfo[kcuu1]:-}" $'\e[A' $'\eOA'; do
    [[ -n $seq ]] || continue
    bindkey "$seq" remnix-suggest-prev
  done
  bindkey '^N' remnix-suggest-next
  bindkey '^P' remnix-suggest-prev
  bindkey '\e' remnix-suggest-dismiss
}
`
}

func zshQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func zshOverlayMenu() string {
	return `
__remnix_overlay_complete_begin() {
  emulate -L zsh
  local prefix=$1
  [[ -n ${REMNIX_SESSION_ID:-} ]] || return 1
  __remnix_rpc_write suggest-complete-interactive "$prefix" "$PWD" "$REMNIX_SESSION_ID"
}

__remnix_overlay_complete_finish() {
  emulate -L zsh
  local n=$1
  shift
  __remnix_rpc_write "$n" "$@" && __remnix_rpc_read
}

__remnix_overlay_complete() {
  emulate -L zsh
  local prefix=$1 n=$2
  shift 2
  [[ -n ${REMNIX_SESSION_ID:-} ]] || return 1
  if __remnix_rpc suggest-complete-interactive "$prefix" "$PWD" "$REMNIX_SESSION_ID" "$n" "$@"; then
    return 0
  fi
  if [[ -n ${__remnix_attach:-} ]]; then
    REPLY=$("$__remnix_attach" --rpc suggest-complete-interactive "$prefix" "$PWD" "$REMNIX_SESSION_ID" "$n" "$@" 2>/dev/null) || return 1
    return 0
  fi
  return 1
}

remnix-suggest-menu() {
  local selected run=0 tmp drilled=0 again=1 started=0
  local -a items
  __remnix_suggest_suppress=1
  if (( ${+functions[__remnix_suggest_clear]} )); then
    __remnix_suggest_clear
    zle redisplay
  fi
  zle -I
  while (( again )); do
    again=0
    items=()
    started=0
    if __remnix_overlay_complete_begin "$BUFFER"; then
      started=1
    fi
    if (( ${+functions[__remnix_suggest_completions]} )); then
      __remnix_suggest_completions
      __remnix_comp_applied_lines "$BUFFER"
      items=("${__remnix_comp_lines[@]}")
    fi
    if (( started )); then
      if (( $#items == 0 && drilled )); then
        __remnix_overlay_complete_finish -1 || true
        break
      fi
      local -a payload descrs
      local -i i
      descrs=("${__remnix_comp_line_descrs[@]}")
      payload=()
      for (( i=1; i<=$#items; i++ )); do
        payload+=("${items[i]}" "${descrs[i]:-}")
      done
      __remnix_overlay_complete_finish "$#items" "${payload[@]}" || break
    elif (( $#items == 0 )); then
      (( drilled )) && break
      if ! __remnix_overlay suggest-interactive "$BUFFER"; then
        __remnix_widget_run suggest --interactive --prefix "$BUFFER" --cwd "$PWD" || break
      fi
    else
      local -a payload descrs
      local -i i
      descrs=("${__remnix_comp_line_descrs[@]}")
      payload=()
      for (( i=1; i<=$#items; i++ )); do
        payload+=("${items[i]}" "${descrs[i]:-}")
      done
      if ! __remnix_overlay_complete "$BUFFER" "$#items" "${payload[@]}"; then
        tmp="${TMPDIR:-/tmp}/remnix-suggest-$$"
        : >"$tmp" 2>/dev/null || tmp=""
        if [[ -z $tmp ]]; then
          break
        fi
        for (( i=1; i<=$#items; i++ )); do
          print -r -- "${items[i]}"$'\t'"${descrs[i]:-}"
        done >"$tmp"
        __remnix_widget_run suggest --interactive --prefix "$BUFFER" --cwd "$PWD" --items-file "$tmp" || {
          rm -f "$tmp"
          break
        }
        rm -f "$tmp"
      fi
    fi
    selected="$REPLY"
    if [[ "$selected" == __remnix_continue__:* ]]; then
      selected="${selected#__remnix_continue__:}"
      [[ -n $selected ]] || break
      LBUFFER="$selected"
      [[ $LBUFFER == *[[:space:]] ]] || LBUFFER="$LBUFFER "
      RBUFFER=""
      zle redisplay
      drilled=1
      again=1
      continue
    fi
    if [[ "$selected" == __remnix_accept__:* ]]; then
      selected="${selected#__remnix_accept__:}"
      run=1
    fi
  done
  if [[ -n "$selected" ]]; then
    LBUFFER="$selected"
    RBUFFER=""
  fi
  if (( ${+functions[__remnix_suggest_clear]} )); then
    __remnix_suggest_clear
    zle redisplay
    [[ -n ${terminfo[ed]:-} ]] && echoti ed
  fi
  zle reset-prompt
  if (( run )); then
    zle .accept-line
  else
    __remnix_suggest_suppress=0
  fi
}
zle -N remnix-suggest-menu
bindkey '^@' remnix-suggest-menu
bindkey -M viins '^@' remnix-suggest-menu
`
}
