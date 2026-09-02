package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/generations"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
	"github.com/mistweaverco/syncsh/internal/crypto/piv"
	"github.com/mistweaverco/syncsh/internal/crypto/recovery"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/sync/equalize"
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
	return a.TombstoneEntries(entries)
}

func (a *App) TombstoneEntries(entries []history.Entry) error {
	if len(entries) == 0 {
		return nil
	}
	eng := a.LocalEngine()
	for _, e := range entries {
		if err := eng.EnqueueHistoryTombstoned(e); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) newEngine(tr transport.Transport, endpointID string, secret []byte, tokens []piv.Token, fido []fido2.Device) *syncer.Engine {
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
		Transport:  tr,
		EndpointID: endpointID,
	})
}

func (a *App) Engine(secret []byte, tokens []piv.Token, fido []fido2.Device) (*syncer.Engine, error) {
	eps := a.Config.Sync.EnabledEndpoints()
	if len(eps) == 0 {
		return a.newEngine(nil, "", secret, tokens, fido), nil
	}
	tr, err := a.OpenTransport(eps[0])
	if err != nil {
		return nil, err
	}
	return a.newEngine(tr, eps[0].ID, secret, tokens, fido), nil
}

func (a *App) EngineFor(ep config.Endpoint, secret []byte, tokens []piv.Token, fido []fido2.Device) (*syncer.Engine, error) {
	tr, err := a.OpenTransport(ep)
	if err != nil {
		return nil, err
	}
	return a.newEngine(tr, ep.ID, secret, tokens, fido), nil
}

func (a *App) OpenTransport(ep config.Endpoint) (transport.Transport, error) {
	switch ep.Type {
	case "", config.TypeDirectory:
		p := config.Expand(ep.Path)
		if p == "" {
			return nil, fmt.Errorf("endpoint %s: directory path is not configured; run syncsh setup", ep.ID)
		}
		return directory.New(p), nil
	case config.TypeRsync:
		work := filepath.Join(config.DataDir(), "stage-rsync-"+ep.ID)
		spec := config.Expand(ep.Remote)
		if spec == "" {
			spec = config.Expand(ep.Path)
		}
		if spec == "" {
			return nil, fmt.Errorf("endpoint %s: rsync remote is not configured; run syncsh setup", ep.ID)
		}
		return rsync.New(spec, work), nil
	case config.TypeSCP:
		work := filepath.Join(config.DataDir(), "stage-scp-"+ep.ID)
		host := config.Expand(ep.Host)
		if host == "" {
			return nil, fmt.Errorf("endpoint %s: scp host is not configured; run syncsh setup", ep.ID)
		}
		return scp.New(host, config.Expand(ep.User), config.Expand(ep.Path), work, ep.Port), nil
	case config.TypeRclone:
		name := ep.RcloneRemote
		if name == "" {
			name = ep.ID
		}
		if err := rclonetr.Init(config.RcloneConfigPath()); err != nil {
			return nil, err
		}
		rclonetr.HardenRemote(name)
		return rclonetr.Open(context.Background(), name, ep.Path)
	default:
		return nil, fmt.Errorf("endpoint %s: unknown type %q", ep.ID, ep.Type)
	}
}

func (a *App) resolveEndpoints(onlyID string) ([]config.Endpoint, error) {
	if onlyID != "" {
		ep, ok := a.Config.Sync.Endpoint(onlyID)
		if !ok {
			return nil, fmt.Errorf("endpoint %q not found", onlyID)
		}
		return []config.Endpoint{ep}, nil
	}
	eps := a.Config.Sync.EnabledEndpoints()
	if len(eps) == 0 {
		return nil, transport.ErrSyncDisabled
	}
	return eps, nil
}

type openedEndpoint struct {
	ep config.Endpoint
	tr transport.Transport
}

func (a *App) openEndpoints(eps []config.Endpoint) ([]openedEndpoint, []error) {
	var live []openedEndpoint
	var errs []error
	for _, ep := range eps {
		tr, err := a.OpenTransport(ep)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ep.ID, err))
			continue
		}
		live = append(live, openedEndpoint{ep: ep, tr: tr})
	}
	return live, errs
}

func (a *App) syncUnlocked(ctx context.Context, secret []byte, tokens []piv.Token, fido []fido2.Device, onlyID string) (anyOK bool, err error) {
	eps, err := a.resolveEndpoints(onlyID)
	if err != nil {
		return false, err
	}
	live, errs := a.openEndpoints(eps)
	for _, o := range live {
		eng := a.newEngine(o.tr, o.ep.ID, secret, tokens, fido)
		if err := eng.Sync(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", o.ep.ID, err))
			continue
		}
		anyOK = true
	}
	if anyOK && onlyID == "" && len(live) > 1 {
		named := make([]equalize.Named, 0, len(live))
		for _, o := range live {
			named = append(named, equalize.Named{ID: o.ep.ID, Transport: o.tr})
		}
		if err := equalize.Equalize(ctx, named); err != nil {
			errs = append(errs, err)
		}
	}
	return anyOK, errors.Join(errs...)
}

func (a *App) Sync(ctx context.Context, secret []byte, tokens []piv.Token, fido []fido2.Device) error {
	return a.SyncOnly(ctx, "", secret, tokens, fido)
}

func (a *App) SyncOnly(ctx context.Context, endpointID string, secret []byte, tokens []piv.Token, fido []fido2.Device) error {
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	anyOK, syncErr := a.syncUnlocked(ctx, secret, tokens, fido, endpointID)
	if !anyOK {
		return syncErr
	}
	return errors.Join(syncErr, runCallbacks(ctx, a.Config.Sync.Callbacks))
}

func (a *App) SyncEngine(ctx context.Context, secret []byte, tokens []piv.Token, fido []fido2.Device) error {
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	anyOK, err := a.syncUnlocked(ctx, secret, tokens, fido, "")
	if !anyOK {
		return err
	}
	return err
}

func (a *App) RunCallbacks(ctx context.Context) error {
	return runCallbacks(ctx, a.Config.Sync.Callbacks)
}

func (a *App) forEachEndpoint(ctx context.Context, secret []byte, tokens []piv.Token, fido []fido2.Device, fn func(ep config.Endpoint, eng *syncer.Engine) error) error {
	eps, err := a.resolveEndpoints("")
	if err != nil {
		return err
	}
	live, errs := a.openEndpoints(eps)
	anyOK := false
	for _, o := range live {
		eng := a.newEngine(o.tr, o.ep.ID, secret, tokens, fido)
		if err := fn(o.ep, eng); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", o.ep.ID, err))
			continue
		}
		anyOK = true
	}
	if !anyOK && len(errs) > 0 {
		return errors.Join(errs...)
	}
	return errors.Join(errs...)
}

func (a *App) RetireDevice(ctx context.Context, id string, secret []byte, tokens []piv.Token, fido []fido2.Device) error {
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	return a.forEachEndpoint(ctx, secret, tokens, fido, func(_ config.Endpoint, eng *syncer.Engine) error {
		return eng.RetireDevice(ctx, id, time.Now().UTC())
	})
}

func (a *App) PruneDevice(ctx context.Context, id string, secret []byte, tokens []piv.Token, fido []fido2.Device) error {
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	return a.forEachEndpoint(ctx, secret, tokens, fido, func(_ config.Endpoint, eng *syncer.Engine) error {
		return eng.PruneDevice(ctx, id)
	})
}

func (a *App) PublishGeneration(ctx context.Context, m generations.Manifest, smk []byte, secret []byte, tokens []piv.Token, fido []fido2.Device) error {
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	return a.forEachEndpoint(ctx, secret, tokens, fido, func(_ config.Endpoint, eng *syncer.Engine) error {
		return eng.PublishGeneration(ctx, m, smk)
	})
}

func (a *App) RecoverGeneration(ctx context.Context, generationID string, secret []byte, tokens []piv.Token, fido []fido2.Device) (generations.Manifest, error) {
	lock, err := AcquireLock()
	if err != nil {
		return generations.Manifest{}, err
	}
	defer func() { _ = lock.Release() }()
	eps, err := a.resolveEndpoints("")
	if err != nil {
		return generations.Manifest{}, err
	}
	live, errs := a.openEndpoints(eps)
	var active generations.Manifest
	var recoveredID string
	for _, o := range live {
		eng := a.newEngine(o.tr, o.ep.ID, secret, tokens, fido)
		got, err := eng.RecoverGeneration(ctx, generationID)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", o.ep.ID, err))
			continue
		}
		active = got
		recoveredID = o.ep.ID
		break
	}
	if recoveredID == "" {
		if len(errs) == 0 {
			return generations.Manifest{}, fmt.Errorf("no endpoints available")
		}
		return generations.Manifest{}, errors.Join(errs...)
	}
	smks, _ := keyring.Get(a.Config.DeviceID)
	smk := smks[active.GenerationID]
	for _, o := range live {
		if o.ep.ID == recoveredID {
			continue
		}
		eng := a.newEngine(o.tr, o.ep.ID, secret, tokens, fido)
		if err := eng.PublishGeneration(ctx, active, smk); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", o.ep.ID, err))
		}
	}
	return active, errors.Join(errs...)
}
