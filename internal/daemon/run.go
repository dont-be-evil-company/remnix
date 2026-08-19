package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/mistweaverco/syncsh/internal/app"
	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
	"github.com/mistweaverco/syncsh/internal/redact"
	"github.com/mistweaverco/syncsh/internal/transport"
)

type Status struct {
	OK         bool   `json:"ok"`
	At         int64  `json:"at"`
	HumanAt    string `json:"human_at"`
	Error      string `json:"error,omitempty"`
	Class      string `json:"class,omitempty"`
	Hint       string `json:"hint,omitempty"`
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
	authFails := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			class := runOnce(ctx)
			delay := interval
			if class == string(transport.HealthAuthRequired) {
				authFails++
				if authFails > 5 {
					authFails = 5
				}
				delay = interval * time.Duration(1<<authFails)
			} else {
				authFails = 0
			}
			timer.Reset(delay)
		}
	}
}

func runOnce(ctx context.Context) string {
	a, err := app.Open()
	if err != nil {
		writeStatus(failStatus(err, ""))
		slog.Error("syncsh daemon: open", "err", err)
		return ""
	}
	defer a.Close()
	if !a.Config.Sync.IsEnabled() {
		writeStatus(Status{OK: true, At: time.Now().Unix(), HumanAt: time.Now().Format(time.RFC3339), Class: "disabled"})
		return "disabled"
	}
	smks, err := keyring.Get(a.Config.DeviceID)
	if err != nil {
		writeStatus(failStatus(err, ""))
		slog.Error("syncsh daemon: keyring", "err", err)
		return ""
	}
	if len(smks) == 0 {
		msg := "keyring empty; run syncsh unlock (recovery key or FIDO touch once)"
		writeStatus(Status{OK: false, At: time.Now().Unix(), Error: msg, HumanAt: time.Now().Format(time.RFC3339)})
		slog.Warn(msg)
		return ""
	}
	if err := a.SyncEngine(ctx, nil, nil, []fido2.Device{}); err != nil {
		if interrupted(ctx, err) {
			slog.Info("syncsh daemon: sync interrupted", "err", err)
			return ""
		}
		class, hint := classifyErr(err, a.Config)
		writeStatus(failStatus(err, class, hint))
		slog.Error("syncsh daemon: sync", "err", err, "class", class)
		return class
	}
	if err := a.MaybeCheckpoint(ctx); err != nil {
		slog.Warn("syncsh daemon: checkpoint", "err", err)
	}
	plan, err := a.GarbageCollect(ctx, false)
	if err != nil {
		if interrupted(ctx, err) {
			slog.Info("syncsh daemon: gc interrupted", "err", err)
			return ""
		}
		writeStatus(failStatus(fmt.Errorf("gc: %w", err), ""))
		slog.Error("syncsh daemon: gc", "err", err)
		return ""
	}
	if n := plan.DeletedCount(); n > 0 {
		slog.Info("syncsh daemon: gc", "deleted", n, "checkpoint", plan.Checkpoint)
	}
	lock, err := app.AcquireLock()
	if err != nil {
		st := failStatus(err, "")
		st.GCDeleted = plan.DeletedCount()
		st.GCEligible = plan.Eligible
		writeStatus(st)
		slog.Error("syncsh daemon: callback lock", "err", err)
		return ""
	}
	cbErr := a.RunCallbacks(ctx)
	_ = lock.Release()
	if cbErr != nil {
		st := failStatus(cbErr, "")
		st.GCDeleted = plan.DeletedCount()
		st.GCEligible = plan.Eligible
		writeStatus(st)
		slog.Error("syncsh daemon: callback", "err", cbErr)
		return ""
	}
	writeStatus(Status{OK: true, At: time.Now().Unix(), GCDeleted: plan.DeletedCount(), GCEligible: plan.Eligible, HumanAt: time.Now().Format(time.RFC3339)})
	return ""
}

func classifyErr(err error, cfg *config.Config) (class, hint string) {
	switch {
	case errors.Is(err, transport.ErrAuthRequired):
		hint = "run: syncsh remote reconnect <name>"
		if cfg != nil && cfg.Sync.Rclone != nil {
			for _, r := range cfg.Sync.Rclone.Remotes {
				if r.ID == cfg.Sync.Rclone.Primary && r.Provider == "icloud-drive" {
					hint = "iCloud authentication expired; run: syncsh remote reconnect " + r.ID
				}
			}
		}
		return string(transport.HealthAuthRequired), hint
	case errors.Is(err, transport.ErrOffline):
		return string(transport.HealthOffline), ""
	case errors.Is(err, transport.ErrSyncDisabled):
		return "disabled", ""
	default:
		return "", ""
	}
}

func failStatus(err error, class string, hint ...string) Status {
	h := ""
	if len(hint) > 0 {
		h = hint[0]
	}
	msg := ""
	if err != nil {
		msg = redact.String(err.Error())
	}
	return Status{
		OK:      false,
		At:      time.Now().Unix(),
		Error:   msg,
		Class:   class,
		Hint:    h,
		HumanAt: time.Now().Format(time.RFC3339),
	}
}

func interrupted(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() != nil
}

// RecordOK stores a successful sync so `daemon status` is not stuck on an
// older canceled/failed tick (for example after `syncsh device add`).
func RecordOK() {
	now := time.Now()
	writeStatus(Status{OK: true, At: now.Unix(), HumanAt: now.Format(time.RFC3339), Class: "manual"})
}

func writeStatus(st Status) {
	st.Error = redact.String(st.Error)
	st.Hint = redact.String(st.Hint)
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
