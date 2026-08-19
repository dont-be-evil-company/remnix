package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
	"github.com/mistweaverco/syncsh/internal/crypto/piv"
	"github.com/mistweaverco/syncsh/internal/crypto/recovery"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/sync/syncer"
	"github.com/mistweaverco/syncsh/internal/transport"
	"github.com/mistweaverco/syncsh/internal/transport/directory"
	rclonetr "github.com/mistweaverco/syncsh/internal/transport/rclone"
	"github.com/mistweaverco/syncsh/internal/transport/rsync"
	"github.com/mistweaverco/syncsh/internal/transport/scp"
)

func (a *App) EnqueueHistoryCreated(e history.Entry) error {
	return a.LocalEngine().EnqueueHistoryCreated(e)
}

// LocalEngine records sync events in SQLite without opening a remote
// transport. History start/end must not touch rclone; the login daemon syncs later.
func (a *App) LocalEngine() *syncer.Engine {
	host, _ := os.Hostname()
	return syncer.New(a.DB, syncer.Options{
		DeviceID:   a.Config.DeviceID,
		DeviceName: a.Config.DeviceName,
		Hostname:   host,
	})
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
	if !a.Config.Sync.IsEnabled() {
		return nil, transport.ErrSyncDisabled
	}
	switch a.Config.Sync.Transport {
	case "", "directory":
		p := config.Expand(a.Config.Sync.Directory.Path)
		if p == "" {
			return nil, fmt.Errorf("sync directory path is not configured; run syncsh setup")
		}
		return directory.New(p), nil
	case "rsync":
		work := filepath.Join(config.DataDir(), "stage-rsync")
		return rsync.New(config.Expand(a.Config.Sync.Rsync.Remote), work), nil
	case "scp":
		work := filepath.Join(config.DataDir(), "stage-scp")
		c := a.Config.Sync.SCP
		return scp.New(config.Expand(c.Host), config.Expand(c.User), config.Expand(c.Path), work, c.Port), nil
	case "rclone":
		rc := a.Config.Sync.Rclone
		if rc == nil {
			return nil, fmt.Errorf("rclone transport is not configured; run syncsh config")
		}
		id := rc.Primary
		var rem *config.RemoteConfig
		for i := range rc.Remotes {
			if rc.Remotes[i].ID == id || (id == "" && rc.Remotes[i].Enabled) {
				rem = &rc.Remotes[i]
				break
			}
		}
		if rem == nil {
			return nil, fmt.Errorf("rclone primary remote is not configured; run syncsh config")
		}
		if err := rclonetr.Init(config.RcloneConfigPath()); err != nil {
			return nil, err
		}
		rclonetr.HardenRemote(rem.RcloneRemote)
		return rclonetr.Open(context.Background(), rem.RcloneRemote, rem.Path)
	case "none":
		return nil, transport.ErrSyncDisabled
	default:
		return nil, fmt.Errorf("unknown transport %q", a.Config.Sync.Transport)
	}
}

func (a *App) Sync(ctx context.Context, secret []byte, tokens []piv.Token, fido []fido2.Device) error {
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	eng, err := a.Engine(secret, tokens, fido)
	if err != nil {
		return err
	}
	if err := eng.Sync(ctx); err != nil {
		return err
	}
	return runCallbacks(ctx, a.Config.Sync.Callbacks)
}

func (a *App) SyncEngine(ctx context.Context, secret []byte, tokens []piv.Token, fido []fido2.Device) error {
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	eng, err := a.Engine(secret, tokens, fido)
	if err != nil {
		return err
	}
	return eng.Sync(ctx)
}

func (a *App) RunCallbacks(ctx context.Context) error {
	return runCallbacks(ctx, a.Config.Sync.Callbacks)
}

func (a *App) RetireDevice(ctx context.Context, id string, secret []byte, tokens []piv.Token, fido []fido2.Device) error {
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	eng, err := a.Engine(secret, tokens, fido)
	if err != nil {
		return err
	}
	return eng.RetireDevice(ctx, id, time.Now().UTC())
}

func (a *App) PruneDevice(ctx context.Context, id string, secret []byte, tokens []piv.Token, fido []fido2.Device) error {
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	eng, err := a.Engine(secret, tokens, fido)
	if err != nil {
		return err
	}
	return eng.PruneDevice(ctx, id)
}
