package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mistweaverco/syncsh/internal/config"
)

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
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			errs = append(errs, fmt.Errorf("callback %d: %w", i+1, err))
		}
	}
	return errors.Join(errs...)
}
