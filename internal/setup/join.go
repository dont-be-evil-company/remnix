package setup

import (
	"context"
	"fmt"

	"charm.land/huh/v2"
	"github.com/mistweaverco/syncsh/internal/app"
	"github.com/mistweaverco/syncsh/internal/repository"
	"github.com/mistweaverco/syncsh/internal/tui/wizard"
)

func RequireValidRemote(ctx context.Context, a *app.App) error {
	eps := a.Config.Sync.EnabledEndpoints()
	if len(eps) == 0 {
		return fmt.Errorf("join requires a remote")
	}
	tr, err := a.OpenTransport(eps[0])
	if err != nil {
		return err
	}
	rep, err := repository.Probe(ctx, tr)
	if err != nil {
		return err
	}
	if rep.Result != repository.Valid {
		return fmt.Errorf("join requires a valid syncsh repository (got %s): %s", rep.Result, rep.Message)
	}
	return nil
}

func ConfigureJoin(ctx context.Context, a *app.App) error {
	cfg := a.Config
	if cfg.DeviceName == "" {
		_ = huh.NewForm(huh.NewGroup(huh.NewInput().Title("Device name").Value(&cfg.DeviceName))).RunWithContext(ctx)
	}
	ep, err := wizard.AddEndpoint(ctx, cfg, true)
	if err != nil {
		return err
	}
	if ep.ID == "" {
		return fmt.Errorf("joining requires a remote")
	}
	on := true
	cfg.Sync.Enabled = &on
	cfg.Sync.UpsertEndpoint(ep)
	if err := cfg.Save(); err != nil {
		return err
	}
	a.Config = cfg
	return RequireValidRemote(ctx, a)
}

func NeedsJoinWizard(a *app.App) bool {
	if a == nil || a.Config == nil || !a.Config.Sync.IsEnabled() {
		return true
	}
	return len(a.Config.Sync.EnabledEndpoints()) == 0
}
