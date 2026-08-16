package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
	"github.com/mistweaverco/syncsh/internal/crypto/piv"
	"github.com/mistweaverco/syncsh/internal/crypto/recovery"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/sync/syncer"
	"github.com/mistweaverco/syncsh/internal/transport"
	"github.com/mistweaverco/syncsh/internal/transport/directory"
	"github.com/mistweaverco/syncsh/internal/transport/rsync"
	"github.com/mistweaverco/syncsh/internal/transport/scp"
)

func (a *App) EnqueueHistoryCreated(e history.Entry) error {
	eng, err := a.Engine(nil, nil, nil)
	if err != nil {
		return err
	}
	return eng.EnqueueHistoryCreated(e)
}

func (a *App) TombstoneCommand(command string) error {
	if command == "" {
		return nil
	}
	entries, err := history.NewStore(a.DB).ListByCommand(command)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	eng, err := a.Engine(nil, nil, nil)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := eng.EnqueueHistoryTombstoned(e); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) Engine(secret []byte, tokens []piv.Token, fido []fido2.Device) (*syncer.Engine, error) {
	tr, err := a.Transport()
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	if len(secret) == 0 {
		if env := os.Getenv("SYNCSH_RECOVERY_KEY"); env != "" {
			secret, _ = recovery.Decode(env)
		}
	}
	cached, _ := keyring.Get(a.Config.DeviceID)
	return syncer.New(a.DB, syncer.Options{
		DeviceID:       a.Config.DeviceID,
		DeviceName:     a.Config.DeviceName,
		Hostname:       host,
		RecoverySecret: secret,
		Tokens:         tokens,
		FIDO2:          fido,
		CachedSMKs:     cached,
		StoreSMKs: func(smks map[string][]byte) {
			_ = keyring.Set(a.Config.DeviceID, smks)
		},
		Transport: tr,
	}), nil
}

func (a *App) Transport() (transport.Transport, error) {
	switch a.Config.Sync.Transport {
	case "", "directory":
		p := a.Config.Sync.Directory.Path
		if p == "" {
			return nil, fmt.Errorf("sync directory path is not configured; run syncsh setup")
		}
		return directory.New(p), nil
	case "rsync":
		work := filepath.Join(config.DataDir(), "stage-rsync")
		return rsync.New(a.Config.Sync.Rsync.Remote, work), nil
	case "scp":
		work := filepath.Join(config.DataDir(), "stage-scp")
		c := a.Config.Sync.SCP
		return scp.New(c.Host, c.User, c.Path, work, c.Port), nil
	default:
		return nil, fmt.Errorf("unknown transport %q", a.Config.Sync.Transport)
	}
}

func (a *App) Sync(ctx context.Context, secret []byte, tokens []piv.Token, fido []fido2.Device) error {
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Release()
	eng, err := a.Engine(secret, tokens, fido)
	if err != nil {
		return err
	}
	return eng.Sync(ctx)
}
