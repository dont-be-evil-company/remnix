package helpparse

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
)

const probeTimeout = 4 * time.Second

// Probe runs argv plus --help, then the help subcommand, then -h.
// It keeps the text with the most described (non-flag) commands so a stub
// like `aws --help` does not hide `aws help`.
func Probe(ctx context.Context, argv []string) (string, error) {
	return ProbeWithPATH(ctx, argv, "")
}

// ProbeWithPATH is Probe, resolving argv[0] on path (the caller's PATH).
// An empty path uses the current process PATH.
func ProbeWithPATH(ctx context.Context, argv []string, path string) (string, error) {
	if len(argv) == 0 {
		return "", fmt.Errorf("empty argv")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, probeTimeout)
		defer cancel()
	}
	var best string
	bestN := -1
	var lastErr error
	for _, args := range helpArgvVariants(argv) {
		out, err := runHelp(ctx, args[0], args[1:], path)
		if err != nil {
			lastErr = err
		}
		if out == "" {
			continue
		}
		n := describedCommands(Parse(out))
		rank := helpPageRank(out, n)
		if rank > bestN {
			bestN = rank
			best = out
		}
		if n >= 3 || NodeSummary(out) != "" {
			return out, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	if best != "" {
		return best, nil
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("no help output")
}

func helpArgvVariants(argv []string) [][]string {
	base := append([]string{}, argv...)
	return [][]string{
		append(append([]string{}, base...), "--help"),
		append(append([]string{}, base...), "help"),
		append(append([]string{}, base...), "-h"),
	}
}

func describedCommands(ents []Entity) int {
	n := 0
	for _, e := range ents {
		if e.Descr != "" && !strings.HasPrefix(e.Name, "-") {
			n++
		}
	}
	return n
}

// helpPageRank prefers a page with a NAME/DESCRIPTION blurb over a usage stub
// that lists the same (or fewer) described commands. describedCommands stays
// the early-return signal so a rich parent --help still wins quickly.
func helpPageRank(out string, described int) int {
	n := described * 10
	if NodeSummary(out) != "" {
		n += 100
	}
	return n
}

func runHelp(ctx context.Context, bin string, args []string, path string) (string, error) {
	cmd := exec.CommandContext(ctx, lookPath(bin, path), args...)
	cmd.Stdin = nil
	cmd.Env = probeEnv(path)
	isolateHelpCmd(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	picked := pickStream(stdout.String(), stderr.String())
	if picked == "" {
		return "", err
	}
	return picked, nil
}

func lookPath(name, path string) string {
	if name == "" || strings.ContainsRune(name, os.PathSeparator) {
		return name
	}
	if path == "" {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
		return name
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		cand := filepath.Join(dir, name)
		if isHelpBin(cand) {
			return cand
		}
	}
	return name
}

func isHelpBin(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return st.Mode()&0o111 != 0
}

func probeEnv(path string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+8)
	skip := map[string]bool{
		"PAGER": true, "MANPAGER": true, "GIT_PAGER": true, "AWS_PAGER": true,
		"CI": true, "CLOUDSDK_CORE_DISABLE_PROMPTS": true,
	}
	if path != "" {
		skip["PATH"] = true
	}
	for _, e := range env {
		key, _, _ := strings.Cut(e, "=")
		if skip[key] {
			continue
		}
		out = append(out, e)
	}
	if path != "" {
		out = append(out, "PATH="+path)
	}
	return append(out,
		"PAGER=cat",
		"MANPAGER=cat",
		"GIT_PAGER=cat",
		"AWS_PAGER=cat",
		"CI=1",
		"CLOUDSDK_CORE_DISABLE_PROMPTS=1",
	)
}

func pickStream(stdout, stderr string) string {
	so, se := looksLikeHelp(stdout), looksLikeHelp(stderr)
	switch {
	case so && !se:
		return stdout
	case se && !so:
		return stderr
	case so && se:
		if helpScore(stderr) > helpScore(stdout) {
			return stderr
		}
		return stdout
	}
	if strings.TrimSpace(stdout) != "" {
		return stdout
	}
	return strings.TrimSpace(stderr)
}

func looksLikeHelp(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	return helpScore(s) >= 2
}

func helpScore(s string) int {
	low := strings.ToLower(s)
	n := 0
	for _, m := range []string{
		"usage", "available commands", "available services", "options:", "flags:",
		"--help", "synopsis", "commands:", "groups",
		"show this help", "show this message",
		"group is one of the following", "command is one of the following",
	} {
		if strings.Contains(low, m) {
			n++
		}
	}
	flagLines := 0
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "-") && (strings.Contains(t, "  ") || strings.Contains(t, ",")) {
			flagLines++
		}
	}
	if flagLines >= 2 {
		n += 2
	} else if flagLines == 1 {
		n++
	}
	letters := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	if letters > 40 {
		n++
	}
	return n
}
