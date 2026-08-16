package app

import (
	"context"

	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/device"
	"github.com/mistweaverco/syncsh/internal/sync/gc"
)

func (a *App) GarbageCollect(ctx context.Context, dryRun bool) (gc.Plan, error) {
	lock, err := AcquireLock()
	if err != nil {
		return gc.Plan{}, err
	}
	defer lock.Release()
	tr, err := a.Transport()
	if err != nil {
		return gc.Plan{}, err
	}
	devs, err := device.NewStore(a.DB).List()
	if err != nil {
		return gc.Plan{}, err
	}
	plan, err := gc.Evaluate(ctx, tr, gc.RequiredDevices(devs), "")
	if err != nil {
		return plan, err
	}
	return plan, gc.Execute(ctx, tr, plan, dryRun)
}

func (a *App) MaybeCheckpoint(ctx context.Context) error {
	tr, err := a.Transport()
	if err != nil {
		return err
	}
	due, err := gc.CheckpointDue(ctx, tr, a.Config.Sync.GCIntervalDuration())
	if err != nil || !due {
		return err
	}
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer lock.Release()
	eng, err := a.Engine(nil, nil, []fido2.Device{})
	if err != nil {
		return err
	}
	smks, active, err := eng.Unlock()
	if err != nil {
		return err
	}
	m, err := eng.CreateCheckpoint(ctx, smks[active.GenerationID], active.GenerationID)
	if err != nil {
		return err
	}
	return eng.PublishAck(ctx, m.ID)
}
