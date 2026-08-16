package wizard

import (
	"context"
	"fmt"
	"strings"

	"charm.land/huh/v2"
	"github.com/dont-be-evil-company/remnix/internal/config"
)

func RunConfig(ctx context.Context, cfg *config.Config) error {
	for {
		action := "save"
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("Configuration").Options(
				huh.NewOption("Enable or disable synchronization", "enable"),
				huh.NewOption("Manage sync endpoints", "endpoints"),
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
		case "endpoints":
			if err := editEndpoints(ctx, cfg); err != nil {
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

func editEndpoints(ctx context.Context, cfg *config.Config) error {
	for {
		action := "back"
		opts := []huh.Option[string]{
			huh.NewOption("Add an endpoint", "add"),
		}
		for _, ep := range cfg.Sync.Endpoints {
			label := ep.ID + " (" + ep.Type + ")"
			if !ep.Enabled {
				label += " disabled"
			}
			opts = append(opts, huh.NewOption("Remove "+label, "rm:"+ep.ID))
		}
		opts = append(opts, huh.NewOption("Back", "back"))
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("Sync endpoints").Options(opts...).Value(&action),
		))
		if err := form.RunWithContext(ctx); err != nil {
			return err
		}
		switch {
		case action == "back":
			return nil
		case action == "add":
			ep, err := AddEndpoint(ctx, cfg, false)
			if err != nil {
				return err
			}
			if ep.ID == "" {
				continue
			}
			on := true
			cfg.Sync.Enabled = &on
			cfg.Sync.UpsertEndpoint(ep)
		case strings.HasPrefix(action, "rm:"):
			cfg.Sync.RemoveEndpoint(strings.TrimPrefix(action, "rm:"))
		}
	}
}
