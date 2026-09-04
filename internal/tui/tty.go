//go:build !windows

package tui

import (
	"os"
	"os/signal"
	"syscall"
)

// prepareWidgetTTY ignores SIGTTOU/SIGTTIN around MakeRaw. Atuin's widget
// stays in the shell's foreground pgrp under $(...); this is only a guard if
// a caller still launches the TUI from a background group.
func prepareWidgetTTY() {
	signal.Ignore(syscall.SIGTTIN, syscall.SIGTTOU)
}

func openTTY() (*os.File, *os.File, func(), error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	return f, f, func() { _ = f.Close() }, nil
}
