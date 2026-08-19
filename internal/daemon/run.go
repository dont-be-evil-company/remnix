package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mistweaverco/syncsh/internal/app"
	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
)

type Status struct {
	OK         bool   `json:"ok"`
	At         int64  `json:"at"`
	HumanAt    string `json:"human_at"`
	Error      string `json:"error,omitempty"`
	GCDeleted  int    `json:"gc_deleted,omitempty"`
	GCEligible bool   `json:"gc_eligible,omitempty"`
}

func Run(ctx context.Context) error {
	interval := time.Minute
	if cfg, err := config.Load(); err == nil {
		interval = cfg.Sync.IntervalDuration()
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			runOnce(ctx)
			timer.Reset(interval)
		}
	}
}

func runOnce(ctx context.Context) {
	a, err := app.Open()
	if err != nil {
		writeStatus(Status{OK: false, At: time.Now().Unix(), Error: err.Error(), HumanAt: time.Now().Format(time.RFC3339)})
		slog.Error("syncsh daemon: open", "err", err)
		return
	}
	defer a.Close()
	smks, err := keyring.Get(a.Config.DeviceID)
	if err != nil {
		writeStatus(Status{OK: false, At: time.Now().Unix(), Error: err.Error(), HumanAt: time.Now().Format(time.RFC3339)})
		slog.Error("syncsh daemon: keyring", "err", err)
		return
	}
	if len(smks) == 0 {
		msg := "keyring empty; run syncsh unlock (recovery key or FIDO touch once)"
		writeStatus(Status{OK: false, At: time.Now().Unix(), Error: msg, HumanAt: time.Now().Format(time.RFC3339)})
		slog.Warn(msg)
		return
	}
	if err := a.SyncEngine(ctx, nil, nil, []fido2.Device{}); err != nil {
		writeStatus(Status{OK: false, At: time.Now().Unix(), Error: err.Error(), HumanAt: time.Now().Format(time.RFC3339)})
		slog.Error("syncsh daemon: sync", "err", err)
		return
	}
	if err := a.MaybeCheckpoint(ctx); err != nil {
		slog.Warn("syncsh daemon: checkpoint", "err", err)
	}
	plan, err := a.GarbageCollect(ctx, false)
	if err != nil {
		writeStatus(Status{OK: false, At: time.Now().Unix(), Error: "gc: " + err.Error(), HumanAt: time.Now().Format(time.RFC3339)})
		slog.Error("syncsh daemon: gc", "err", err)
		return
	}
	if n := plan.DeletedCount(); n > 0 {
		slog.Info("syncsh daemon: gc", "deleted", n, "checkpoint", plan.Checkpoint)
	}
	lock, err := app.AcquireLock()
	if err != nil {
		writeStatus(Status{OK: false, At: time.Now().Unix(), Error: err.Error(), HumanAt: time.Now().Format(time.RFC3339), GCDeleted: plan.DeletedCount(), GCEligible: plan.Eligible})
		slog.Error("syncsh daemon: callback lock", "err", err)
		return
	}
	cbErr := a.RunCallbacks(ctx)
	_ = lock.Release()
	if cbErr != nil {
		writeStatus(Status{OK: false, At: time.Now().Unix(), Error: cbErr.Error(), HumanAt: time.Now().Format(time.RFC3339), GCDeleted: plan.DeletedCount(), GCEligible: plan.Eligible})
		slog.Error("syncsh daemon: callback", "err", cbErr)
		return
	}
	writeStatus(Status{OK: true, At: time.Now().Unix(), GCDeleted: plan.DeletedCount(), GCEligible: plan.Eligible, HumanAt: time.Now().Format(time.RFC3339)})
}

func writeStatus(st Status) {
	_ = os.MkdirAll(config.DataDir(), 0o700)
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.WriteFile(config.DaemonStatusPath(), b, 0o600)
}

func ReadStatus() (Status, error) {
	b, err := os.ReadFile(config.DaemonStatusPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Status{}, nil
		}
		return Status{}, err
	}
	var st Status
	if err := json.Unmarshal(b, &st); err != nil {
		return Status{}, err
	}
	return st, nil
}

func Binary() (string, error) {
	bin, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locate syncsh binary: %w", err)
	}
	return bin, nil
}
