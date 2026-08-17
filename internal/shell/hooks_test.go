package shell

import (
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
		`suggest --prefix "$BUFFER"`,
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
	if strings.Contains(on, `zle -A ".$w"`) {
		t.Fatal("builtin accept-line must be wrapped with zle .accept-line, not zle -A")
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
	if strings.Contains(off, "suggest --prefix") || strings.Contains(off, "syncsh-suggest-accept") {
		t.Fatal("disabled suggest should not emit inline suggestion hooks")
	}
}
