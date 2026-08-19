// Package wizard is the reusable setup/config/remote TUI (huh + picker + rclone SM).
package wizard

import (
	"context"
	"os"

	"charm.land/huh/v2"
	"github.com/mistweaverco/syncsh/internal/app"
	"github.com/mistweaverco/syncsh/internal/repository"
	"github.com/mistweaverco/syncsh/internal/tui/picker"
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
				huh.NewOption("Create a new syncsh history", IntentCreate),
				huh.NewOption("Join an existing syncsh history", IntentJoin),
				huh.NewOption("Use syncsh locally without synchronization", IntentLocal),
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
			Title("How should syncsh synchronize your encrypted history?").
			Description("rclone gives syncsh direct access to cloud and network storage while your history remains encrypted by syncsh. No syncsh account or server is required.").
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

func ProbeOrWarn(ctx context.Context, a *app.App) (repository.Report, error) {
	tr, err := a.Transport()
	if err != nil {
		return repository.Report{}, err
	}
	return repository.Probe(ctx, tr)
}
