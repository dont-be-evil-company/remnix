// Package wizard is the reusable setup/config/remote TUI (huh + picker + rclone SM).
package wizard

import (
	"context"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/dont-be-evil-company/remnix/internal/app"
	"github.com/dont-be-evil-company/remnix/internal/config"
	"github.com/dont-be-evil-company/remnix/internal/repository"
	"github.com/dont-be-evil-company/remnix/internal/tui/picker"
)

type Intent int

const (
	IntentCreate Intent = iota
	IntentJoin
	IntentLocal
	IntentExit
)

type Outcome struct {
	Intent     Intent
	Canceled   bool
	JoinRemote bool
}

func ChooseIntent(ctx context.Context, preselect Intent) (Intent, error) {
	intent := preselect
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[Intent]().
			Title("What would you like to do?").
			Options(
				huh.NewOption("Create a new remnix history", IntentCreate),
				huh.NewOption("Join an existing remnix history", IntentJoin),
				huh.NewOption("Use remnix locally without synchronization", IntentLocal),
				huh.NewOption("Exit", IntentExit),
			).
			Value(&intent),
	))
	if err := form.RunWithContext(ctx); err != nil {
		return IntentExit, err
	}
	return intent, nil
}

func ChooseTransport(ctx context.Context) (string, error) {
	tr := "rclone"
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("How should remnix synchronize your encrypted history?").
			Description("rclone gives remnix direct access to cloud and network storage while your history remains encrypted by remnix. No remnix account or server is required.").
			Options(
				huh.NewOption("rclone (recommended)", "rclone"),
				huh.NewOption("Synced/local folder", "directory"),
				huh.NewOption("rsync", "rsync"),
				huh.NewOption("scp", "scp"),
				huh.NewOption("No synchronization", "none"),
			).
			Value(&tr),
	))
	if err := form.RunWithContext(ctx); err != nil {
		return "", err
	}
	return tr, nil
}

func PickLocalDir(ctx context.Context, title string, confirm bool) (picker.Result, error) {
	start, _ := os.UserHomeDir()
	return picker.Run(picker.NewLocal(start), picker.Options{Title: title, Start: start, ConfirmDest: confirm, CreateMode: confirm})
}

func uniqueEndpointID(cfg *config.Config, want string) string {
	if _, ok := cfg.Sync.Endpoint(want); !ok {
		return want
	}
	for i := 2; ; i++ {
		id := fmt.Sprintf("%s-%d", want, i)
		if _, ok := cfg.Sync.Endpoint(id); !ok {
			return id
		}
	}
}

func AddEndpoint(ctx context.Context, cfg *config.Config, join bool) (config.Endpoint, error) {
	tr, err := ChooseTransport(ctx)
	if err != nil {
		return config.Endpoint{}, err
	}
	switch tr {
	case "none":
		return config.Endpoint{}, nil
	case "directory":
		title := "Select remnix storage folder"
		if join {
			title = "Select existing remnix folder"
		}
		res, err := PickLocalDir(ctx, title, !join)
		if err != nil {
			return config.Endpoint{}, err
		}
		if res.Canceled {
			return config.Endpoint{}, fmt.Errorf("cancelled")
		}
		if !join && res.Join {
			return config.Endpoint{}, fmt.Errorf("an existing repository was selected; use 'remnix device add' to join")
		}
		ep := config.DirectoryEndpoint(uniqueEndpointID(cfg, "local"), res.Path)
		return ep, nil
	case "rclone":
		return ConfigureRcloneRemoteMode(ctx, cfg, join)
	case "rsync":
		var remote string
		_ = huh.NewForm(huh.NewGroup(huh.NewInput().Title("rsync remote").Value(&remote))).RunWithContext(ctx)
		return config.Endpoint{ID: uniqueEndpointID(cfg, "rsync"), Type: config.TypeRsync, Remote: remote, Enabled: true}, nil
	case "scp":
		ep := config.Endpoint{Type: config.TypeSCP, Enabled: true}
		_ = huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Host").Value(&ep.Host),
			huh.NewInput().Title("User").Value(&ep.User),
			huh.NewInput().Title("Path").Value(&ep.Path),
		)).RunWithContext(ctx)
		id := ep.Host
		if id == "" {
			id = "scp"
		}
		ep.ID = uniqueEndpointID(cfg, id)
		return ep, nil
	default:
		return config.Endpoint{}, fmt.Errorf("unknown endpoint type %q", tr)
	}
}

func ProbeOrWarn(ctx context.Context, a *app.App) (repository.Report, error) {
	eps := a.Config.Sync.EnabledEndpoints()
	if len(eps) == 0 {
		return repository.Report{}, fmt.Errorf("no endpoints configured")
	}
	tr, err := a.OpenTransport(eps[0])
	if err != nil {
		return repository.Report{}, err
	}
	return repository.Probe(ctx, tr)
}
