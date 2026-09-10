//go:build unix

package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/config"
)

func (s *Server) watchSignals(ctx context.Context) {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, syscall.SIGHUP)
	defer signal.Stop(ch)
	timer := time.NewTimer(s.configWatchInterval())
	defer timer.Stop()
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
				slog.Warn("remnix daemon: reload config", "err", err)
			} else {
				slog.Info("remnix daemon: config reloaded")
			}
		case <-timer.C:
			st, err := os.Stat(config.ConfigPath())
			if err != nil {
				timer.Reset(s.configWatchInterval())
				continue
			}
			if st.ModTime().After(mtime) {
				mtime = st.ModTime()
				if err := s.ReloadConfig(); err != nil {
					slog.Warn("remnix daemon: reload config", "err", err)
				}
			}
			timer.Reset(s.configWatchInterval())
		}
	}
}
