package app

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dont-be-evil-company/remnix/internal/config"
	"github.com/gofrs/flock"
)

type Lock struct {
	flock *flock.Flock
}

func AcquireLock() (*Lock, error) {
	path := config.LockPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	l := flock.New(path)
	locked, err := l.TryLock()
	if err != nil {
		return nil, fmt.Errorf("acquire lock: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("another remnix process holds %s; if the daemon is syncing, wait or run: remnix daemon status --watch", path)
	}
	return &Lock{flock: l}, nil
}

func (l *Lock) Release() error {
	if l == nil || l.flock == nil {
		return nil
	}
	return l.flock.Unlock()
}
