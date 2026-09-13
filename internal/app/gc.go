package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/dont-be-evil-company/remnix/internal/crypto/fido2"
	"github.com/dont-be-evil-company/remnix/internal/device"
	"github.com/dont-be-evil-company/remnix/internal/progress"
	"github.com/dont-be-evil-company/remnix/internal/sync/equalize"
	"github.com/dont-be-evil-company/remnix/internal/sync/gc"
	"github.com/dont-be-evil-company/remnix/internal/sync/syncer"
	"github.com/dont-be-evil-company/remnix/internal/transport"
)

func (a *App) GarbageCollect(ctx context.Context, dryRun bool) (gc.Plan, error) {
	progress.Report(ctx, "collecting garbage")
	lock, err := AcquireLock()
	if err != nil {
		return gc.Plan{}, err
	}
	defer func() { _ = lock.Release() }()
	devs, err := device.NewStore(a.DB).List()
	if err != nil {
		return gc.Plan{}, err
	}
	required := gc.RequiredDevices(devs)
	eps, err := a.resolveEndpoints("")
	if err != nil {
		return gc.Plan{}, err
	}
	live, errs := a.openEndpoints(eps)
	var combined gc.Plan
	anyOK := false
	for _, o := range live {
		if err := transport.WithSession(ctx, o.tr, func() error {
			if !dryRun {
				if err := transport.Dedupe(ctx, o.tr); err != nil {
					return err
				}
			}
			plan, err := gc.Evaluate(ctx, o.tr, required, "")
			if err != nil {
				return err
			}
			if err := gc.Execute(ctx, o.tr, plan, dryRun); err != nil {
				return err
			}
			combined.Bundles = append(combined.Bundles, plan.Bundles...)
			combined.Checkpoints = append(combined.Checkpoints, plan.Checkpoints...)
			combined.Generations = append(combined.Generations, plan.Generations...)
			combined.TmpOrphans = append(combined.TmpOrphans, plan.TmpOrphans...)
			combined.BlockedBy = append(combined.BlockedBy, plan.BlockedBy...)
			if plan.Checkpoint != "" {
				combined.Checkpoint = plan.Checkpoint
			}
			combined.Eligible = combined.Eligible || plan.Eligible
			return nil
		}); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", o.ep.ID, err))
			continue
		}
		anyOK = true
	}
	if !anyOK && len(errs) > 0 {
		return combined, errors.Join(errs...)
	}
	return combined, errors.Join(errs...)
}

func (a *App) MaybeCheckpoint(ctx context.Context) error {
	return a.checkpoint(ctx, false)
}

func (a *App) ForceCheckpoint(ctx context.Context) error {
	return a.checkpoint(ctx, true)
}

func (a *App) checkpoint(ctx context.Context, force bool) error {
	eps, err := a.resolveEndpoints("")
	if err != nil {
		return err
	}
	live, openErrs := a.openEndpoints(eps)
	due := force
	if !force {
		for _, o := range live {
			var ok bool
			err := transport.WithSession(ctx, o.tr, func() error {
				var e error
				ok, e = gc.CheckpointDue(ctx, o.tr, a.Config.Sync.GCIntervalDuration())
				return e
			})
			if err != nil {
				openErrs = append(openErrs, fmt.Errorf("%s: %w", o.ep.ID, err))
				continue
			}
			if ok {
				due = true
			}
		}
	}
	if !due {
		return errors.Join(openErrs...)
	}
	progress.Report(ctx, "writing checkpoint")
	lock, err := AcquireLock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	if len(live) == 0 {
		return errors.Join(openErrs...)
	}
	eng := a.newEngine(live[0].tr, live[0].ep.ID, nil, nil, []fido2.Device{})
	smks, active, err := eng.Unlock()
	if err != nil {
		return err
	}
	m, err := eng.CreateCheckpoint(ctx, smks[active.GenerationID], active.GenerationID)
	if err != nil {
		if !force && errors.Is(err, syncer.ErrCheckpointNotCaughtUp) {
			return errors.Join(openErrs...)
		}
		return err
	}
	if len(live) > 1 {
		named := make([]equalize.Named, 0, len(live))
		for _, o := range live {
			named = append(named, equalize.Named{ID: o.ep.ID, Transport: o.tr})
		}
		if err := equalize.Equalize(ctx, named); err != nil {
			openErrs = append(openErrs, err)
		}
	}
	for _, o := range live {
		e := a.newEngine(o.tr, o.ep.ID, nil, nil, []fido2.Device{})
		if err := e.PublishAck(ctx, m.ID); err != nil {
			openErrs = append(openErrs, fmt.Errorf("%s: %w", o.ep.ID, err))
		}
	}
	return errors.Join(openErrs...)
}
