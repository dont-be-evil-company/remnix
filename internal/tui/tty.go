//go:build !windows

package tui

import "os"

func openTTY() (*os.File, *os.File, func(), error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	return f, f, func() { _ = f.Close() }, nil
}
