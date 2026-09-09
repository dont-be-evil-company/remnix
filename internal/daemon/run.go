package daemon

import (
	"context"

	"github.com/dont-be-evil-company/remnix/internal/app"
	"github.com/dont-be-evil-company/remnix/internal/history"
)

// runOnce is a test helper that opens a temporary app and runs one sync cycle.
func runOnce(ctx context.Context) string {
	a, err := app.Open()
	if err != nil {
		writeStatus(failStatus(err, ""))
		return ""
	}
	defer a.Close()
	s := &Server{app: a, history: history.NewService(history.NewStore(a.DB), history.NewCache(), a.DB.SQL, a.Config.DeviceID, nil, nil)}
	return s.runSyncCycle(ctx)
}
