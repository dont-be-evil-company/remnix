//go:build !unix

package ptyproxy

import "fmt"

// Run is not available on non-Unix platforms.
func Run(shellPath string) error {
	return fmt.Errorf("pty-proxy is not supported on this platform")
}

// ExitError is a child's non-zero exit status.
type ExitError struct {
	Code int
}

func (e ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}
