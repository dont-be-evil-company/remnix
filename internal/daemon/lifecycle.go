package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"

	"github.com/mistweaverco/syncsh/internal/config"
)

func Run(ctx context.Context) error {
	if err := Ping(); err == nil {
		return nil
	}
	s := NewServer()
	if err := s.Open(); err != nil {
		return err
	}
	defer func() { _ = s.Close() }()
	if err := s.listen(); err != nil {
		if err := Ping(); err == nil {
			return nil
		}
		return err
	}
	if err := writePID(); err != nil {
		slog.Warn("syncsh daemon: pid file", "err", err)
	}
	defer removePID()
	go s.rebuildCache()
	go s.syncer.Run(ctx)
	go s.serveTerminal(ctx)
	go s.watchSignals(ctx)
	return s.serveControl(ctx)
}

func (s *Server) listen() error {
	if err := os.MkdirAll(config.RuntimeDir(), 0o700); err != nil {
		return err
	}
	ctrl, err := listenUnix(config.ControlSocketPath())
	if err != nil {
		return err
	}
	term, err := listenUnix(config.TerminalSocketPath())
	if err != nil {
		_ = ctrl.Close()
		return err
	}
	s.controlLn = ctrl
	s.termLn = term
	return nil
}

func listenUnix(path string) (net.Listener, error) {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(true)
	}
	return ln, nil
}

func writePID() error {
	return os.WriteFile(config.PIDFilePath(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
}

func removePID() {
	_ = os.Remove(config.PIDFilePath())
}

func Ping() error {
	conn, err := net.DialTimeout("unix", config.ControlSocketPath(), pingTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	return pingConn(conn)
}

func Binary() (string, error) {
	bin, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate syncsh binary: %w", err)
	}
	return bin, nil
}
