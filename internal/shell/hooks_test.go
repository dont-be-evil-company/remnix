package shell

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestIntegrationSupported(t *testing.T) {
	opts := Options{SuggestEnabled: true, SuggestAccept: []string{"Right"}}
	for _, sh := range []string{"zsh", "bash", "fish", "nu"} {
		out, err := Integration(sh, "syncsh", opts)
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		hasLegacy := strings.Contains(out, "history start") && strings.Contains(out, "history end")
		hasRPC := strings.Contains(out, "--rpc start") && strings.Contains(out, "--rpc end")
		hasZshRPC := strings.Contains(out, "__syncsh_rpc start")
		if !hasLegacy && !hasRPC && !hasZshRPC {
			t.Fatalf("%s hook missing lifecycle commands", sh)
		}
		if sh == "zsh" && !strings.Contains(out, `"$__syncsh_bin" daemon`) {
			t.Fatal("zsh hook must start the daemon")
		}
		if sh == "bash" && !strings.Contains(out, `__syncsh_agent_ensure`) {
			t.Fatal("bash hook must start the history agent")
		}
		if sh == "fish" && !strings.Contains(out, `__syncsh_agent_ensure`) {
			t.Fatal("fish hook must start the history agent")
		}
		if !strings.Contains(out, "search --interactive") {
			t.Fatalf("%s hook missing interactive search fallback", sh)
		}
		if !strings.Contains(out, "search-interactive") {
			t.Fatalf("%s hook missing daemon overlay search RPC", sh)
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
	overlayAt := strings.Index(searchFn, `search-interactive`)
	tuiAt := strings.Index(searchFn, `search --interactive`)
	if clearAt < 0 || redisplayAt < 0 || invalidateAt < 0 || overlayAt < 0 || tuiAt < 0 {
		t.Fatal("syncsh-search missing clear/redisplay/zle -I before overlay/TUI")
	}
	if overlayAt < invalidateAt {
		t.Fatal("zle -I must precede daemon overlay RPC")
	}
	if !strings.Contains(searchFn, "__syncsh_overlay search-interactive") {
		t.Fatal("syncsh-search must try daemon overlay RPC first")
	}
	if !strings.Contains(searchFn, "__syncsh_widget_run search --interactive") {
		t.Fatal("syncsh-search must fall back to the local TUI via __syncsh_widget_run")
	}
	if !strings.Contains(zsh, "3>&1 1>&2 2>&3 3>&-") {
		t.Fatal("widget runner must swap stdout/stderr like Atuin so the TUI owns the TTY")
	}
	if strings.Contains(zsh, "--result-file") {
		t.Fatal("zsh must not use --result-file; Atuin prints the selection on stderr")
	}
	if strings.Contains(searchFn, `selected="$("$__syncsh_bin" search --interactive`) {
		t.Fatal("syncsh-search must not capture TUI stdout with command substitution")
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
	if !strings.Contains(bashHook, "3>&1 1>&2 2>&3 3>&-") {
		t.Fatal("bash widget must swap stdout/stderr like Atuin")
	}
	if strings.Contains(bashHook, "--result-file") {
		t.Fatal("bash must not use --result-file")
	}
	fishHook, _ := Integration("fish", "syncsh", opts)
	if !strings.Contains(fishHook, `__syncsh_bind \cr`) {
		t.Fatal("fish missing Ctrl+R bind")
	}
	if strings.Contains(fishHook, `\c@`) {
		t.Fatal("fish 4 rejects bind \\c@ as an invalid token and aborts source")
	}
	if !strings.Contains(fishHook, "commandline -f execute") {
		t.Fatal("fish should run the selected command")
	}
	if !strings.Contains(fishHook, "commandline --current-buffer --replace -- ''") {
		t.Fatal("fish must clear the pre-widget buffer so Ctrl+R does not leave typed leftovers")
	}
	if strings.Contains(fishHook, "3>&1 1>&2 2>&3 3>&-") {
		t.Fatal("fish must not fd-swap; command substitution steals stdout")
	}
	if strings.Contains(fishHook, "(__syncsh_widget_run") {
		t.Fatal("fish must not wrap the TUI in command substitution")
	}
	if !strings.Contains(fishHook, "--result-file") {
		t.Fatal("fish must return the selection via --result-file")
	}
	if !strings.Contains(fishHook, "--result-file $tmp </dev/tty >/dev/tty") {
		t.Fatal("fish bind captures stdout; the TUI must reopen /dev/tty for overlay")
	}
	if strings.Count(fishHook, "commandline -f repaint") < 2 {
		t.Fatal("fish must repaint after cancel as well as after a selection")
	}
	nuHook, _ := Integration("nu", "syncsh", opts)
	if !strings.Contains(nuHook, "keycode: char_r") {
		t.Fatal("nu missing Ctrl+R keybinding")
	}
	if !strings.Contains(nuHook, "syncsh-search") {
		t.Fatal("nu missing search command")
	}
	if !strings.Contains(nuHook, "--result-file") {
		t.Fatal("nu keeps --result-file; it cannot fd-swap like bash/zsh")
	}
	if strings.Contains(nuHook, "source (syncsh init") {
		t.Fatal("nu must not source a subexpression; source requires a literal path")
	}
	if strings.Contains(nuHook, "let selected = (do {") {
		t.Fatal("nu must not wrap the TUI in a capturing (do { ... })")
	}
	if !strings.Contains(nuHook, "o> /dev/tty") {
		t.Fatal("nu must send the TUI to /dev/tty; executehostcommand captures stdout")
	}
	if !strings.Contains(nuHook, "commandline edit --replace --accept") {
		t.Fatal("nu must execute on Enter via commandline edit --accept")
	}
	if !strings.Contains(nuHook, "source ~/.cache/syncsh.nu") {
		t.Fatal("nu comment must tell the user to source a literal cache file")
	}
	if !strings.Contains(nuHook, "env.nu") {
		t.Fatal("nu comment must generate the cache file from env.nu; source is parse-time")
	}
	if !strings.Contains(nuHook, "__syncsh_rebind") || !strings.Contains(nuHook, "where {|k|") {
		t.Fatal("nu must replace the default history_menu Ctrl+R, not only append")
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
		`"$__syncsh_bin" daemon`,
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
		"__syncsh_suggest_fetch_ghost",
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
		"__syncsh_rpc suggest",
		"__syncsh_menu_hl_sel",
		"__syncsh_menu_hl_desc",
		"fg=#F5C2E7",
		"bg=#313244",
		"fg=#CDD6F4",
		"█",
		"░",
	} {
		if !strings.Contains(on, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(on, "__syncsh_suggest_kinds+=(history)") {
		t.Fatal("LSP menu must not list history rows")
	}
	if strings.Contains(on, "suggest-list") || strings.Contains(on, "__syncsh_rpc_list") || strings.Contains(on, "--list") {
		t.Fatal("LSP menu must not fetch suggest-list")
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
		"__syncsh_comp_applied_lines",
		"__syncsh_comp_line_descrs",
		`BUFFER="$BUFFER "`,
		`${rest//[[:space:]]/}`,
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
	if strings.Contains(on, "__syncsh_suggest_kinds+=(history)") {
		t.Fatal("LSP menu must not list history rows")
	}
	if strings.Contains(on, "__syncsh_suggest_fetch_hist") || strings.Contains(on, "suggest-list") {
		t.Fatal("completions menu must not fetch history rows")
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

func TestPtyProxyPreamble(t *testing.T) {
	off, err := Integration("zsh", "/opt/syncsh", Options{SuggestEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(off, "pty-proxy") {
		t.Fatal("preamble must be omitted when pty_proxy is off")
	}
	on, err := Integration("zsh", "/opt/syncsh", Options{SuggestEnabled: true, PtyProxyEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(on, "exec '/opt/syncsh-attach'") {
		t.Fatal("zsh missing attach exec")
	}
	if !strings.Contains(on, "SYNCSH_PTY_PROXY_ACTIVE") {
		t.Fatal("missing active guard")
	}
	bashOn, _ := Integration("bash", "/opt/syncsh", Options{PtyProxyEnabled: true})
	if !strings.Contains(bashOn, `--shell "$BASH"`) {
		t.Fatal("bash preamble should forward $BASH")
	}
	fishOn, _ := Integration("fish", "/opt/syncsh", Options{PtyProxyEnabled: true})
	if !strings.Contains(fishOn, "--shell (status fish-path)") {
		t.Fatal("fish preamble should forward fish-path")
	}
	nuOn, _ := Integration("nu", "/opt/syncsh", Options{PtyProxyEnabled: true})
	if !strings.Contains(nuOn, "--shell $nu.current-exe") {
		t.Fatal("nu preamble should forward current-exe")
	}
}

func TestZshOverlayMenuWhenProxy(t *testing.T) {
	post, err := Integration("zsh", "syncsh", Options{SuggestEnabled: true, SuggestMenu: true, SuggestAccept: []string{"Right"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(post, "__syncsh_suggest_menu_box") {
		t.Fatal("without proxy, POSTDISPLAY menu must remain")
	}
	if strings.Contains(post, "suggest --interactive") {
		t.Fatal("without proxy, overlay suggest TUI should not bind")
	}
	over, err := Integration("zsh", "syncsh", Options{
		SuggestEnabled:  true,
		SuggestMenu:     true,
		PtyProxyEnabled: true,
		SuggestAccept:   []string{"Right"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(over, "__syncsh_suggest_menu_box") {
		t.Fatal("proxy-on must not emit POSTDISPLAY menu")
	}
	if !strings.Contains(over, `_main_complete`) || !strings.Contains(over, `__syncsh_comp_applied_lines`) {
		t.Fatal("proxy-on overlay must capture compsys completions")
	}
	if !strings.Contains(over, `BUFFER="$BUFFER "`) {
		t.Fatal("command-only buffers must retry compsys with a trailing space")
	}
	if strings.Contains(over, "bindkey $'\\t' syncsh-suggest-complete") {
		t.Fatal("proxy-on must not bind Tab to the POSTDISPLAY completer")
	}
	if !strings.Contains(over, "suggest --interactive") || !strings.Contains(over, "bindkey '^@'") {
		t.Fatal("proxy-on must bind Ctrl+Space overlay menu")
	}
	if !strings.Contains(over, "__syncsh_overlay_complete_begin") {
		t.Fatal("proxy-on Ctrl+Space must open the overlay before compsys so a spinner can paint")
	}
	menuAt := strings.Index(over, "syncsh-suggest-menu()")
	if menuAt < 0 {
		t.Fatal("missing syncsh-suggest-menu")
	}
	menu := over[menuAt:]
	beginAt := strings.Index(menu, "__syncsh_overlay_complete_begin")
	compAt := strings.Index(menu, "__syncsh_suggest_completions")
	if beginAt < 0 || compAt < 0 || beginAt > compAt {
		t.Fatal("spinner overlay must start before compsys capture")
	}
	if !strings.Contains(over, "__syncsh_rpc_write") || !strings.Contains(over, "__syncsh_overlay_complete_finish") {
		t.Fatal("proxy-on must stream completion items after the overlay is open")
	}
	if !strings.Contains(over, "__syncsh_continue__:") {
		t.Fatal("proxy-on Ctrl+Space must drill into a suggestion and reload completions")
	}
	if !strings.Contains(over, `[[ $LBUFFER == *[[:space:]] ]] || LBUFFER="$LBUFFER "`) {
		t.Fatal("ctrl+space continue must add a trailing space so the next completer runs")
	}
	if !strings.Contains(over, "suggest-interactive") {
		t.Fatal("proxy-on Ctrl+Space must try daemon overlay RPC")
	}
	if !strings.Contains(over, "POSTDISPLAY") {
		t.Fatal("ghost text must remain with the overlay menu")
	}
}

func TestBashBleGhostAndOverlayMenu(t *testing.T) {
	out, err := Integration("bash", "syncsh", Options{SuggestEnabled: true, SuggestMenu: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "ble/complete/auto-complete/source:syncsh") {
		t.Fatal("bash should register a ble.sh autosuggest source")
	}
	if !strings.Contains(out, `__syncsh_rpc suggest`) {
		t.Fatal("ble.sh source should use agent RPC")
	}
	if !strings.Contains(out, "suggest --interactive") || !strings.Contains(out, `\C-@`) {
		t.Fatal("bash should bind Ctrl+Space overlay menu")
	}
	off, _ := Integration("bash", "syncsh", Options{SuggestEnabled: false})
	if strings.Contains(off, "source:syncsh") {
		t.Fatal("disabled suggest should not emit ble.sh source")
	}
}

func TestFishOverlayMenuNoGhost(t *testing.T) {
	out, err := Integration("fish", "syncsh", Options{SuggestEnabled: true, SuggestMenu: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "suggest --interactive") || !strings.Contains(out, `ctrl-space`) {
		t.Fatal("fish should bind Ctrl+Space overlay menu")
	}
	if strings.Contains(out, `\c@`) {
		t.Fatal("fish 4 rejects bind \\c@ as an invalid token and aborts source")
	}
	if strings.Contains(out, "POSTDISPLAY") {
		t.Fatal("fish has no POSTDISPLAY ghost")
	}
}

func TestNuOverlayMenu(t *testing.T) {
	out, err := Integration("nu", "syncsh", Options{SuggestMenu: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "syncsh-suggest-menu") || !strings.Contains(out, "keycode: space") {
		t.Fatal("nu should bind Ctrl+Space overlay menu")
	}
	if !strings.Contains(out, "__syncsh_rebind") {
		t.Fatal("Ctrl+Space must go through __syncsh_rebind")
	}
}

func TestFishInitParses(t *testing.T) {
	bin, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish not installed")
	}
	out, err := Integration("fish", "/opt/syncsh", Options{
		SuggestEnabled:  true,
		SuggestMenu:     true,
		PtyProxyEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/init.fish"
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--no-config", "--no-execute", path)
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fish rejected init: %v\n%s\n%s", err, got, out)
	}
}

func TestNuInitParses(t *testing.T) {
	bin, err := exec.LookPath("nu")
	if err != nil {
		t.Skip("nu not installed")
	}
	out, err := Integration("nu", "/opt/syncsh", Options{
		SuggestEnabled:  true,
		SuggestMenu:     true,
		PtyProxyEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/init.nu"
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--no-config-file", "-c", "nu-check "+path)
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("nu rejected init: %v\n%s\n%s", err, got, out)
	}
}
