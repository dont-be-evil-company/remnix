package ptyproxy

import "os"

const (
	EnvActive = "SYNCSH_PTY_PROXY_ACTIVE"
	EnvSocket = "SYNCSH_PTY_PROXY_SOCKET"
	EnvTmux   = "SYNCSH_PTY_PROXY_TMUX"
)

// Active reports whether this process is running inside syncsh pty-proxy.
func Active() bool {
	return os.Getenv(EnvActive) != ""
}

// SocketPath is the snapshot socket advertised by the wrapping pty-proxy.
func SocketPath() string {
	return os.Getenv(EnvSocket)
}
