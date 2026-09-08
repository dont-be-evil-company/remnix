package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mistweaverco/syncsh/internal/app"
	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
	"github.com/mistweaverco/syncsh/internal/redact"
	"github.com/mistweaverco/syncsh/internal/transport"
)

type SyncScheduler struct {
	server    *Server
	mu        sync.Mutex
	running   bool
	interval  time.Duration
	authFails int
}

func newScheduler(s *Server, interval time.Duration) *SyncScheduler {
	if interval <= 0 {
		interval = time.Minute
	}
	return &SyncScheduler{server: s, interval: interval}
}

func (sc *SyncScheduler) SetInterval(d time.Duration) {
	if d <= 0 {
		d = time.Minute
	}
	sc.mu.Lock()
	sc.interval = d
	sc.mu.Unlock()
}

func (sc *SyncScheduler) Interval() time.Duration {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.interval
}

func (sc *SyncScheduler) Run(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			class := sc.server.runSyncCycle(ctx)
			delay := sc.Interval()
			if class == string(transport.HealthAuthRequired) {
				sc.mu.Lock()
				sc.authFails++
				if sc.authFails > 5 {
					sc.authFails = 5
				}
				n := sc.authFails
				sc.mu.Unlock()
				delay = delay * time.Duration(1<<n)
			} else {
				sc.mu.Lock()
				sc.authFails = 0
				sc.mu.Unlock()
			}
			timer.Reset(delay)
		}
	}
}

func (sc *SyncScheduler) SyncNow(ctx context.Context) string {
	sc.mu.Lock()
	if sc.running {
		sc.mu.Unlock()
		return "busy"
	}
	sc.running = true
	sc.mu.Unlock()
	defer func() {
		sc.mu.Lock()
		sc.running = false
		sc.mu.Unlock()
	}()
	return sc.server.runSyncCycle(ctx)
}

func (s *Server) runSyncCycle(ctx context.Context) string {
	a := s.app
	if a == nil {
		writeStatus(failStatus(errors.New("daemon app not open"), ""))
		return ""
	}
	if !a.Config.Sync.IsEnabled() {
		st := Status{OK: true, At: time.Now().Unix(), HumanAt: time.Now().Format(time.RFC3339), Class: "disabled"}
		writeStatus(st)
		s.setLastSync(st)
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
		st := Status{OK: false, At: time.Now().Unix(), Error: msg, HumanAt: time.Now().Format(time.RFC3339)}
		writeStatus(st)
		s.setLastSync(st)
		slog.Warn(msg)
		return ""
	}
	syncStart := time.Now()
	cs, err := a.SyncEngineWithChanges(ctx, nil, nil, []fido2.Device{})
	syncMs := time.Since(syncStart).Milliseconds()
	if err != nil {
		if interrupted(ctx, err) {
			slog.Info("syncsh daemon: sync interrupted", "err", err)
			return ""
		}
		class, hint := classifyErr(err, a.Config)
		st := failStatus(err, class, hint)
		st.SyncMs = syncMs
		writeStatus(st)
		s.setLastSync(st)
		slog.Error("syncsh daemon: sync", "err", err, "class", class, "sync", FormatElapsed(syncMs))
		return class
	}
	if s.history != nil && !cs.Empty() {
		if err := s.history.ApplyRemoteChanges(cs); err != nil {
			slog.Warn("syncsh daemon: cache after sync", "err", err)
			s.history.Cache().MarkDirty()
			_ = s.history.Rebuild(ctx)
			reclaimMemory()
		}
	}
	if err := a.MaybeCheckpoint(ctx); err != nil {
		slog.Warn("syncsh daemon: checkpoint", "err", err)
	}
	gcStart := time.Now()
	plan, err := a.GarbageCollect(ctx, false)
	gcMs := time.Since(gcStart).Milliseconds()
	if err != nil {
		if interrupted(ctx, err) {
			slog.Info("syncsh daemon: gc interrupted", "err", err)
			return ""
		}
		st := failStatus(fmt.Errorf("gc: %w", err), "")
		st.SyncMs = syncMs
		st.GCMs = gcMs
		writeStatus(st)
		s.setLastSync(st)
		slog.Error("syncsh daemon: gc", "err", err, "sync", FormatElapsed(syncMs), "gc", FormatElapsed(gcMs))
		return ""
	}
	if n := plan.DeletedCount(); n > 0 {
		slog.Info("syncsh daemon: gc", "deleted", n, "checkpoint", plan.Checkpoint, "gc", FormatElapsed(gcMs))
		if s.history != nil {
			s.history.Cache().MarkDirty()
			_ = s.history.Rebuild(ctx)
			reclaimMemory()
		}
	}
	lock, err := app.AcquireLock()
	if err != nil {
		st := failStatus(err, "")
		st.GCDeleted = plan.DeletedCount()
		st.GCEligible = plan.Eligible
		st.SyncMs = syncMs
		st.GCMs = gcMs
		writeStatus(st)
		s.setLastSync(st)
		slog.Error("syncsh daemon: callback lock", "err", err)
		return ""
	}
	cbErr := a.RunCallbacks(ctx)
	_ = lock.Release()
	if cbErr != nil {
		st := failStatus(cbErr, "")
		st.GCDeleted = plan.DeletedCount()
		st.GCEligible = plan.Eligible
		st.SyncMs = syncMs
		st.GCMs = gcMs
		writeStatus(st)
		s.setLastSync(st)
		slog.Error("syncsh daemon: callback", "err", cbErr)
		return ""
	}
	st := Status{
		OK:         true,
		At:         time.Now().Unix(),
		GCDeleted:  plan.DeletedCount(),
		GCEligible: plan.Eligible,
		HumanAt:    time.Now().Format(time.RFC3339),
		SyncMs:     syncMs,
		GCMs:       gcMs,
	}
	writeStatus(st)
	s.setLastSync(st)
	slog.Info("syncsh daemon: tick", "sync", FormatElapsed(syncMs), "gc", FormatElapsed(gcMs))
	return ""
}

func classifyErr(err error, cfg *config.Config) (class, hint string) {
	switch {
	case errors.Is(err, transport.ErrAuthRequired):
		hint = "run: syncsh remote reconnect <name>"
		if cfg != nil {
			for _, r := range cfg.Sync.EnabledEndpoints() {
				if r.Provider == "icloud-drive" && strings.Contains(err.Error(), r.ID) {
					hint = "iCloud authentication expired; run: syncsh remote reconnect " + r.ID
					break
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

func (s *Server) setLastSync(st Status) {
	s.mu.Lock()
	s.lastSync = st
	s.mu.Unlock()
}
