package ptyproxy

import "os"

const (
	EnvActive = "REMNIX_PTY_PROXY_ACTIVE"
	EnvSocket = "REMNIX_PTY_PROXY_SOCKET"
	EnvTmux   = "REMNIX_PTY_PROXY_TMUX"
)

// Active reports whether this process is running inside remnix pty-proxy.
func Active() bool {
	return os.Getenv(EnvActive) != ""
}

// SocketPath is the snapshot socket advertised by the wrapping pty-proxy.
func SocketPath() string {
	return os.Getenv(EnvSocket)
}
