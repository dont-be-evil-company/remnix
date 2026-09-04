//go:build windows

package tui

import "os"

func prepareWidgetTTY() {}

func openTTY() (*os.File, *os.File, func(), error) {
	in, err := os.OpenFile("CONIN$", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, nil, err
	}
	out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0)
	if err != nil {
		_ = in.Close()
		return nil, nil, nil, err
	}
	return in, out, func() {
		_ = in.Close()
		_ = out.Close()
	}, nil
}
