package wizard

import (
	"context"
	"fmt"
	"strings"

	"charm.land/huh/v2"
	"github.com/mistweaverco/syncsh/internal/config"
)

func RunConfig(ctx context.Context, cfg *config.Config) error {
	for {
		action := "save"
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("Configuration").Options(
				huh.NewOption("Enable or disable synchronization", "enable"),
				huh.NewOption("Change transport / remote", "transport"),
				huh.NewOption("Edit post-sync callbacks", "callbacks"),
				huh.NewOption("Save and exit", "save"),
				huh.NewOption("Cancel", "cancel"),
			).Value(&action),
		))
		if err := form.RunWithContext(ctx); err != nil {
			return err
		}
		switch action {
		case "cancel":
			return fmt.Errorf("cancelled")
		case "save":
			return nil
		case "enable":
			enable := cfg.Sync.IsEnabled()
			if err := huh.NewForm(huh.NewGroup(
				huh.NewConfirm().Title("Enable synchronization?").
					Description("Local history is kept either way. Remote data is not deleted.").
					Value(&enable),
			)).RunWithContext(ctx); err != nil {
				return err
			}
			cfg.Sync.Enabled = &enable
			if !enable {
				cfg.Sync.Transport = "none"
			}
		case "transport":
			if err := editTransport(ctx, cfg); err != nil {
				return err
			}
		case "callbacks":
			raw := strings.Join(cfg.Sync.Callbacks, "\n")
			if err := huh.NewForm(huh.NewGroup(
				huh.NewInput().Title("Callbacks (one per line, or | separated)").
					Description("Optional commands after a successful sync. Do not put secrets here.").
					Value(&raw),
			)).RunWithContext(ctx); err != nil {
				return err
			}
			raw = strings.ReplaceAll(raw, "|", "\n")
			var cbs []string
			for _, ln := range strings.Split(raw, "\n") {
				ln = strings.TrimSpace(ln)
				if ln != "" {
					cbs = append(cbs, ln)
				}
			}
			cfg.Sync.Callbacks = cbs
		}
	}
}

func editTransport(ctx context.Context, cfg *config.Config) error {
	tr, err := ChooseTransport(ctx)
	if err != nil {
		return err
	}
	on := true
	cfg.Sync.Enabled = &on
	cfg.Sync.Transport = tr
	switch tr {
	case "directory":
		res, err := PickLocalDir(ctx, "Select syncsh storage folder", false)
		if err != nil {
			return err
		}
		if res.Canceled {
			return fmt.Errorf("cancelled")
		}
		cfg.Sync.Directory.Path = res.Path
	case "rclone":
		return ConfigureRcloneRemote(ctx, cfg)
	case "rsync":
		_ = huh.NewForm(huh.NewGroup(huh.NewInput().Title("rsync remote").Value(&cfg.Sync.Rsync.Remote))).RunWithContext(ctx)
	case "scp":
		_ = huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Host").Value(&cfg.Sync.SCP.Host),
			huh.NewInput().Title("User").Value(&cfg.Sync.SCP.User),
			huh.NewInput().Title("Path").Value(&cfg.Sync.SCP.Path),
		)).RunWithContext(ctx)
	case "none":
		off := false
		cfg.Sync.Enabled = &off
	}
	return nil
}
