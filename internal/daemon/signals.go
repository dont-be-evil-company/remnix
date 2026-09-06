//go:build unix

package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mistweaverco/syncsh/internal/config"
)

func (s *Server) watchSignals(ctx context.Context) {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGHUP)
	defer signal.Stop(ch)
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	var mtime time.Time
	if st, err := os.Stat(config.ConfigPath()); err == nil {
		mtime = st.ModTime()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			if err := s.ReloadConfig(); err != nil {
				slog.Warn("syncsh daemon: reload config", "err", err)
			} else {
				slog.Info("syncsh daemon: config reloaded")
			}
		case <-tick.C:
			st, err := os.Stat(config.ConfigPath())
			if err != nil {
				continue
			}
			if st.ModTime().After(mtime) {
				mtime = st.ModTime()
				if err := s.ReloadConfig(); err != nil {
					slog.Warn("syncsh daemon: reload config", "err", err)
				}
			}
		}
	}
}
