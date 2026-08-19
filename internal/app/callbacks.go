package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/mistweaverco/syncsh/internal/config"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func runCallbacks(ctx context.Context, cmds []string) error {
	var errs []error
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	for i, raw := range cmds {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", config.Expand(raw))
		if home != "" {
			cmd.Dir = home
		}
		var buf bytes.Buffer
		cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
		cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
		if err := cmd.Run(); err != nil {
			if tail := callbackOutputTail(buf.String()); tail != "" {
				errs = append(errs, fmt.Errorf("callback %d: %w: %s", i+1, err, tail))
			} else {
				errs = append(errs, fmt.Errorf("callback %d: %w", i+1, err))
			}
		}
	}
	return errors.Join(errs...)
}

func callbackOutputTail(s string) string {
	s = ansiEscape.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	var pick []string
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		u := strings.ToUpper(ln)
		if strings.Contains(u, "ERROR") || strings.Contains(u, "FAILED") || strings.Contains(u, "ABORT") {
			pick = append(pick, ln)
		}
	}
	if len(pick) == 0 {
		if len(lines) > 3 {
			lines = lines[len(lines)-3:]
		}
		for _, ln := range lines {
			if t := strings.TrimSpace(ln); t != "" {
				pick = append(pick, t)
			}
		}
	} else if len(pick) > 3 {
		pick = pick[len(pick)-3:]
	}
	out := strings.Join(pick, "; ")
	if len(out) > 400 {
		return out[:400] + "…"
	}
	return out
}
