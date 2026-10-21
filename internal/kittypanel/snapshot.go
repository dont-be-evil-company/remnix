// Package kittypanel snapshots a Kitty window and reports whether the
// remote-control socket the height-limited panel needs is available.
package kittypanel

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// cursorCUP is the CUP kitty appends for get-text --add-cursor. Row and
// column are 1-based.
var cursorCUP = regexp.MustCompile(`\x1b\[(\d+);(\d+)H`)

// SplitScreen turns kitty @ get-text output into one string per screen row.
// A trailing newline is the terminator, not an extra blank row.
func SplitScreen(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return []string{}
	}
	return strings.Split(text, "\n")
}

// GetText reads the parent window's visible screen, including ANSI colors.
// cursorRow is 0-based and -1 when kitty did not report a cursor. parentID is
// Kitty's window id; the overlay's own KITTY_WINDOW_ID is not it.
func GetText(parentID string) (lines []string, cursorRow int, err error) {
	listen := os.Getenv("KITTY_LISTEN_ON")
	if listen == "" {
		return nil, -1, fmt.Errorf("KITTY_LISTEN_ON is unset")
	}
	if parentID == "" {
		parentID = os.Getenv("REMNIX_KITTY_PARENT")
	}
	if parentID == "" {
		return nil, -1, fmt.Errorf("kitty parent window id is unset")
	}
	cmd := exec.Command("kitty", "@", "--to", listen, "get-text", "--ansi", "--add-cursor", "--match", "id:"+parentID)
	out, err := cmd.Output()
	if err != nil {
		return nil, -1, fmt.Errorf("kitty get-text: %w", err)
	}
	body, row, _ := stripTrailingCursor(string(out))
	return SplitScreen(body), row, nil
}

// stripTrailingCursor removes the cursor sequences kitty appends after the
// screen text. row is 0-based, or -1 when those sequences are absent.
func stripTrailingCursor(text string) (body string, row int, ok bool) {
	matches := cursorCUP.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, -1, false
	}
	last := matches[len(matches)-1]
	start := last[0]
	for start > 0 && text[start-1] != '\n' {
		start--
	}
	if !onlyCursorSuffix(text[start:]) {
		return text, -1, false
	}
	r, err := strconv.Atoi(text[last[2]:last[3]])
	if err != nil || r < 1 {
		return text, -1, false
	}
	return text[:start], r - 1, true
}

func onlyCursorSuffix(s string) bool {
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && !isCSIFinal(s[i]) {
				i++
			}
			if i < len(s) {
				i++
			}
			continue
		}
		if s[i] == ' ' || s[i] == '\r' {
			i++
			continue
		}
		return false
	}
	return cursorCUP.MatchString(s)
}

func isCSIFinal(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

// Report describes the Kitty listen socket. ok is false when a direct Kitty
// window cannot open the height-limited panel.
func Report() (ok bool, msg string) {
	listen := os.Getenv("KITTY_LISTEN_ON")
	if listen == "" {
		return false, "kitty panel: KITTY_LISTEN_ON is unset; set allow_remote_control and listen_on so Ctrl+R can use the height-limited panel"
	}
	path := listen
	if rest, found := strings.CutPrefix(listen, "unix:"); found {
		path = rest
	}
	if path == "" {
		return false, "kitty panel: KITTY_LISTEN_ON is empty"
	}
	if strings.HasPrefix(path, "@") {
		return true, "kitty panel: listen socket " + listen
	}
	if _, err := os.Stat(path); err != nil {
		return false, "kitty panel: listen socket missing: " + path
	}
	return true, "kitty panel: listen socket " + path
}
