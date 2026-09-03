package shell

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestIntegrationSupported(t *testing.T) {
	opts := Options{SuggestEnabled: true, SuggestAccept: []string{"Right"}}
	for _, sh := range []string{"zsh", "bash", "fish"} {
		out, err := Integration(sh, "syncsh", opts)
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		if !strings.Contains(out, "history start") || !strings.Contains(out, "history end") {
			t.Fatalf("%s hook missing lifecycle commands", sh)
		}
		if sh == "zsh" && !strings.Contains(out, `"$__syncsh_bin" agent`) {
			t.Fatal("zsh hook must start the history agent")
		}
		if !strings.Contains(out, "search --interactive") {
			t.Fatalf("%s hook missing interactive search", sh)
		}
	}
}

func TestCtrlRBindings(t *testing.T) {
	opts := Options{SuggestEnabled: true, SuggestAccept: []string{"Right"}}
	zsh, _ := Integration("zsh", "syncsh", opts)
	if !strings.Contains(zsh, "bindkey '^R'") {
		t.Fatal("zsh missing bindkey ^R")
	}
	if !strings.Contains(zsh, "zle .accept-line") || !strings.Contains(zsh, "__syncsh_accept__:") {
		t.Fatal("zsh should run the selected command")
	}
	if strings.Contains(zsh, "(( run )) && zle accept-line") {
		t.Fatal("nested zle accept-line hits the suggest wrapper with empty WIDGET")
	}
	// Suggest menu must stay dismissed across reset-prompt → .accept-line.
	if !strings.Contains(zsh, "redisplay|reset-prompt") {
		t.Fatal("suggest redraw must clear on redisplay/reset-prompt, not rebuild")
	}
	searchAt := strings.Index(zsh, "syncsh-search() {")
	if searchAt < 0 {
		t.Fatal("missing syncsh-search")
	}
	searchFn := zsh[searchAt:]
	if end := strings.Index(searchFn, "\nzle -N syncsh-search"); end > 0 {
		searchFn = searchFn[:end]
	}
	// clear → redisplay → zle -I must precede the TUI so alt-screen saves a clean buffer.
	clearAt := strings.Index(searchFn, "__syncsh_suggest_clear")
	redisplayAt := strings.Index(searchFn, "zle redisplay")
	invalidateAt := strings.Index(searchFn, "zle -I")
	tuiAt := strings.Index(searchFn, `search --interactive`)
	if clearAt < 0 || redisplayAt < 0 || invalidateAt < 0 || tuiAt < 0 {
		t.Fatal("syncsh-search missing clear/redisplay/zle -I before TUI")
	}
	if clearAt >= redisplayAt || redisplayAt >= invalidateAt || invalidateAt >= tuiAt {
		t.Fatal("syncsh-search must clear, redisplay, then zle -I before opening the TUI")
	}
	if !strings.Contains(searchFn, "echoti ed") {
		t.Fatal("syncsh-search must clear-to-eos after TUI in case alt-screen restored junk")
	}
	// redisplay must come before echoti ed (known cursor), both after the TUI.
	postTui := searchFn[tuiAt:]
	if strings.Index(postTui, "zle redisplay") > strings.Index(postTui, "echoti ed") {
		t.Fatal("after TUI: redisplay before echoti ed")
	}
	if !strings.Contains(zsh, "__syncsh_suggest_suppress") {
		t.Fatal("syncsh-search must suppress suggest redraw during ctrl+r/accept")
	}
	if strings.Contains(zsh, `} always {
    __syncsh_suggest_suppress=0
  }`) || strings.Contains(zsh, `} always {
        __syncsh_suggest_suppress=0
      }`) {
		t.Fatal("suppress must stay set until precmd; clearing in always-block restores ghost under oh-my-posh")
	}
	if !strings.Contains(zsh, `__syncsh_suggest_suppress} )) && __syncsh_suggest_suppress=0`) &&
		!strings.Contains(zsh, "suggest_suppress=0") {
		t.Fatal("precmd must clear suggest suppress after the command")
	}
	menu, _ := Integration("zsh", "syncsh", Options{SuggestEnabled: true, SuggestMenu: true, SuggestAccept: []string{"Right"}})
	if !strings.Contains(menu, `__syncsh_suggest_idx > 0`) {
		t.Fatal("multi-line suggest menu must only paint while navigating (idx>0)")
	}
	bashHook, _ := Integration("bash", "syncsh", opts)
	if !strings.Contains(bashHook, `\C-r`) {
		t.Fatal("bash missing C-r")
	}
	if !strings.Contains(bashHook, "accept-line") {
		t.Fatal("bash should run the selected command")
	}
	fishHook, _ := Integration("fish", "syncsh", opts)
	if !strings.Contains(fishHook, `bind \cr`) {
		t.Fatal("fish missing bind \\cr")
	}
	if !strings.Contains(fishHook, "commandline -f execute") {
		t.Fatal("fish should run the selected command")
	}
}

func TestUnsupported(t *testing.T) {
	if _, err := Integration("tcsh", "syncsh", Options{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestZshInlineSuggest(t *testing.T) {
	on, err := Integration("zsh", "syncsh", Options{SuggestEnabled: true, SuggestAccept: []string{"Right", "Tab"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"POSTDISPLAY",
		"syncsh-suggest-accept",
		`__syncsh_rpc suggest "$BUFFER"`,
		`"$__syncsh_bin" agent`,
		"zsh/net/socket",
		"'Right'",
		"'Tab'",
		"bindkey $'\\t'",
		"__syncsh_suggest_hl",
		"fg=238",
		`BUFFER="$BUFFER$POSTDISPLAY"`,
		"__syncsh_suggest_clear_then_$w",
		"__syncsh_suggest_bind_clear",
		"zle .$w",
		"memo=syncsh-suggest",
	} {
		if !strings.Contains(on, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(on, `suggest --prefix "$LBUFFER"`) {
		t.Fatal("ghost text must match BUFFER; LBUFFER overlaps when the cursor is not at EOL")
	}
	if !strings.Contains(on, "__syncsh_suggest_clear") || strings.Count(on, `BUFFER="$BUFFER$POSTDISPLAY"`) != 1 {
		t.Fatal("Enter must drop ghost text; only the accept widget may merge POSTDISPLAY")
	}
	if strings.Contains(on, "typeset -gi __syncsh_suggest_suppress=1") {
		t.Fatal("suppress must be assigned globally; typeset inside a widget shadows it")
	}
	if strings.Contains(on, `[[ ${widgets[$w]:-} == user:__syncsh_suggest_clear_then_$w ]] && continue`) {
		t.Fatal("accept-line wrapper must be refreshed on re-eval, not skipped when already bound")
	}
	if strings.Contains(on, `__syncsh_suggest_clear
        zle redisplay
        zle $orig`) {
		t.Fatal("accept-line must use zle -R after clear, not zle redisplay")
	}
	if !strings.Contains(on, `__syncsh_suggest_clear
      zle -R
      zle $orig`) {
		t.Fatal("accept-line must clear then zle -R so ghost POSTDISPLAY is not frozen into scrollback")
	}
	if strings.Contains(on, `} always {
        __syncsh_suggest_suppress=0
      }`) {
		t.Fatal("accept-line must not clear suppress in always; oh-my-posh reset-prompt would restore ghost")
	}
	if !strings.Contains(on, "re-eval") && !strings.Contains(on, "always refresh the wrapper body") {
		t.Fatal("accept-line wrapper must be redefined on every init")
	}
	if strings.Contains(on, `zle -A ".$w"`) {
		t.Fatal("builtin accept-line must be wrapped with zle .accept-line, not zle -A")
	}
	if !strings.Contains(on, "add-zle-hook-widget -d line-pre-redraw") {
		t.Fatal("must dedupe line-pre-redraw hooks on re-eval")
	}
	if !strings.Contains(on, "redisplay|reset-prompt") {
		t.Fatal("redraw hook must clear on redisplay/reset-prompt, not rebuild suggestions")
	}
	if strings.Contains(on, "__syncsh_suggest_orig[$WIDGET]") || strings.Contains(on, "__syncsh_suggest_clear_then_orig") {
		t.Fatal("accept-line wrapper must not look up orig via $WIDGET")
	}
	if strings.Contains(on, "]10;?") || strings.Contains(on, "suggest_pick_hl") {
		t.Fatal("init must not query the terminal for colors")
	}
	off, err := Integration("zsh", "syncsh", Options{SuggestEnabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(off, "suggest --prefix") || strings.Contains(off, "syncsh-suggest-accept") || strings.Contains(off, "__syncsh_rpc suggest") {
		t.Fatal("disabled suggest should not emit inline suggestion hooks")
	}
}

func TestZshSuggestMenu(t *testing.T) {
	off, err := Integration("zsh", "syncsh", Options{SuggestEnabled: true, SuggestAccept: []string{"Right"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(off, "suggest-list") || strings.Contains(off, "syncsh-suggest-next") || strings.Contains(off, "┌") || strings.Contains(off, "#F5C2E7") {
		t.Fatal("menu should be omitted when disabled")
	}
	on, err := Integration("zsh", "syncsh", Options{SuggestEnabled: true, SuggestMenu: true, SuggestAccept: []string{"Right"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"suggest-list",
		"__syncsh_suggest_menu_box",
		"┌",
		"__syncsh_suggest_suffix",
		"__syncsh_suggest_typed",
		"__syncsh_suggest_idx=0",
		"__syncsh_suggest_off=2",
		"__syncsh_suggest_scroll",
		"__syncsh_suggest_commit_selection",
		"__syncsh_suggest_icon_typed",
		"syncsh-suggest-next",
		"syncsh-suggest-prev",
		"syncsh-suggest-dismiss",
		"bindkey '^N'",
		"bindkey '^P'",
		"bindkey '\\e'",
		"__syncsh_rpc_list",
		"[0-9]##",
		"__syncsh_menu_hl_sel",
		"__syncsh_menu_hl_desc",
		"fg=#F5C2E7",
		"bg=#313244",
		"fg=#CDD6F4",
		"--list",
		"█",
		"░",
	} {
		if !strings.Contains(on, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(on, "_main_complete") || strings.Contains(on, "__syncsh_compadd") || strings.Contains(on, "zle -C __syncsh_comp_list") {
		t.Fatal("compsys capture must be omitted when completions are off")
	}
	if strings.Contains(on, "zle -M") {
		t.Fatal("menu must not use zle -M status dumps")
	}
	if strings.Contains(on, "]10;?") || strings.Contains(on, "suggest_pick_hl") {
		t.Fatal("menu must not query the terminal for colors")
	}
}

func TestZshSuggestCompletions(t *testing.T) {
	on, err := Integration("zsh", "syncsh", Options{
		SuggestEnabled:     true,
		SuggestMenu:        true,
		SuggestCompletions: true,
		SuggestAccept:      []string{"Right"},
		IconTyped:          "›",
		IconHistory:        "*",
		IconCompletion:     "+",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"__syncsh_suggest_completions",
		"syncsh-suggest-complete",
		"_main_complete",
		"__syncsh_compadd",
		"list-choices",
		"__syncsh_suggest_kinds+=(completion)",
		"__syncsh_suggest_icon_completion",
		"__syncsh_comp_values >= 512",
		"__syncsh_comp_cache_key",
		"__syncsh_comp_inserts",
		"__syncsh_comp_cache_inserts",
		"__syncsh_suggest_off",
		"█",
		"bindkey $'\\t' syncsh-suggest-complete",
		"${(@P)avar}",
		"${(@P)dvar}",
		"builtin compadd -O matches",
		"opt_p",
		"opt_P",
	} {
		if !strings.Contains(on, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(on, "__syncsh_comp_values >= 64") {
		t.Fatal("compsys capture must not stop at 64")
	}
	if strings.Contains(on, `matches+=("${argv[i,-1]}")`) || strings.Contains(on, `matches+=("${(@)argv[i,-1]}")`) || strings.Contains(on, `descrs=("${(P)argv[i]}")`) {
		t.Fatal("quoted array slices join matches/descriptions into one row")
	}
	if strings.Contains(on, `applied="${__syncsh_comp_lbuffer%$__syncsh_comp_prefix}${m}${__syncsh_comp_rbuffer#$__syncsh_comp_suffix}"`) {
		t.Fatal("accept must insert -p/-P affixes, not the listed match alone")
	}
	if !strings.Contains(on, "k|o|J|V|X|x|W|F|M|E|r|R") {
		t.Fatal("-k must consume the array name, not treat it as a match")
	}
	listAt := strings.Index(on, "__syncsh_comp_list_fn()")
	if listAt < 0 {
		t.Fatal("missing __syncsh_comp_list_fn")
	}
	listFn := on[listAt:]
	mainOff := strings.Index(listFn, "_main_complete")
	snapOff := strings.Index(listFn, `typeset -g __syncsh_comp_prefix="$PREFIX"`)
	if mainOff < 0 || snapOff < 0 || snapOff > mainOff {
		t.Fatal("PREFIX must be snapshotted before _main_complete so _path_files cannot shrink it to the last component")
	}
	if !strings.Contains(on, "syncsh-suggest-complete") {
		t.Fatal("Tab must request compsys completions")
	}
	if strings.Contains(on, "if (( ${+functions[__syncsh_suggest_completions]} )); then") {
		t.Fatal("typing must not capture compsys on every redraw")
	}
	if strings.Contains(on, "zle -M") {
		t.Fatal("menu must not use zle -M status dumps")
	}
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not installed")
	}
	f, err := os.CreateTemp(t.TempDir(), "syncsh-init-*.zsh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(on); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(zsh, "-n", f.Name()).CombinedOutput()
	if err != nil {
		t.Fatalf("zsh -n: %v\n%s", err, out)
	}
}
