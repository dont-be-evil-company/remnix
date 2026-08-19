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
	tr, err := a.Transport()
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
	tr, err := wizard.ChooseTransport(ctx)
	if err != nil {
		return err
	}
	on := true
	cfg.Sync.Enabled = &on
	cfg.Sync.Transport = tr
	switch tr {
	case "none":
		return fmt.Errorf("joining requires a remote")
	case "directory":
		res, err := wizard.PickLocalDir(ctx, "Select existing syncsh folder", true)
		if err != nil {
			return err
		}
		if res.Canceled {
			return fmt.Errorf("cancelled")
		}
		cfg.Sync.Directory.Path = res.Path
	case "rclone":
		if err := wizard.ConfigureRcloneRemoteMode(ctx, cfg, true); err != nil {
			return err
		}
	case "rsync":
		_ = huh.NewForm(huh.NewGroup(huh.NewInput().Title("rsync remote").Value(&cfg.Sync.Rsync.Remote))).RunWithContext(ctx)
	case "scp":
		_ = huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Host").Value(&cfg.Sync.SCP.Host),
			huh.NewInput().Title("User").Value(&cfg.Sync.SCP.User),
			huh.NewInput().Title("Path").Value(&cfg.Sync.SCP.Path),
		)).RunWithContext(ctx)
	}
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
	switch a.Config.Sync.Transport {
	case "", "none":
		return true
	case "directory":
		return a.Config.Sync.Directory.Path == ""
	case "rclone":
		return a.Config.Sync.Rclone == nil || a.Config.Sync.Rclone.Primary == ""
	case "rsync":
		return a.Config.Sync.Rsync.Remote == ""
	case "scp":
		return a.Config.Sync.SCP.Host == ""
	default:
		return false
	}
}
