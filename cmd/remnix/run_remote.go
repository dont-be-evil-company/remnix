package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"charm.land/huh/v2"
	"github.com/dont-be-evil-company/remnix/internal/app"
	"github.com/dont-be-evil-company/remnix/internal/client"
	"github.com/dont-be-evil-company/remnix/internal/crypto/fido2"
	"github.com/dont-be-evil-company/remnix/internal/crypto/keyring"
	"github.com/dont-be-evil-company/remnix/internal/crypto/keys"
	"github.com/dont-be-evil-company/remnix/internal/crypto/piv"
	"github.com/dont-be-evil-company/remnix/internal/crypto/recovery"
	"github.com/dont-be-evil-company/remnix/internal/crypto/rotation"
	"github.com/dont-be-evil-company/remnix/internal/crypto/slots"
	"github.com/dont-be-evil-company/remnix/internal/daemon"
	"github.com/dont-be-evil-company/remnix/internal/device"
	"github.com/dont-be-evil-company/remnix/internal/doctor"
	"github.com/dont-be-evil-company/remnix/internal/progress"
	"github.com/dont-be-evil-company/remnix/internal/protocol"
	"github.com/dont-be-evil-company/remnix/internal/redact"
	"github.com/dont-be-evil-company/remnix/internal/repository"
	"github.com/dont-be-evil-company/remnix/internal/setup"
	"github.com/dont-be-evil-company/remnix/internal/sync/gc"
	"github.com/dont-be-evil-company/remnix/internal/sync/merge"
	"github.com/dont-be-evil-company/remnix/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func recoverySecretFromEnv() []byte {
	if env := os.Getenv("REMNIX_RECOVERY_KEY"); env != "" {
		s, err := recovery.Decode(env)
		if err == nil {
			return s
		}
	}
	return nil
}

func tokensFromHardware() []piv.Token {
	toks, err := (piv.HardwareFactory{}).List()
	if err != nil {
		return nil
	}
	return toks
}

func runSync(cmd *cobra.Command, endpoint string) error {
	sp := tui.StartStatus(cmd.ErrOrStderr())
	ctx := progress.With(cmd.Context(), sp.Set)
	err := runSyncWork(ctx, cmd, endpoint, sp)
	sp.Finish(err)
	if err != nil {
		return err
	}
	daemon.RecordOK()
	return nil
}

func runSyncWork(ctx context.Context, cmd *cobra.Command, endpoint string, sp *tui.Status) error {
	if endpoint == "" {
		sp.Set("waiting for daemon")
		err := waitSyncNow(ctx, true, sp)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return err
		}
		if !shouldLocalSyncFallback(err, daemonControlUp()) {
			return err
		}
	}
	sp.Set("opening local database")
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	var syncErr error
	if endpoint != "" {
		syncErr = a.SyncOnly(ctx, endpoint, recoverySecretFromEnv(), tokensFromHardware(), nil)
	} else {
		syncErr = a.Sync(ctx, recoverySecretFromEnv(), tokensFromHardware(), nil)
	}
	if syncErr != nil {
		return syncErr
	}
	return a.ForceCheckpoint(ctx)
}

const (
	errSyncAlreadyRunning = "sync already running"
	syncNowWait           = 15 * time.Minute
	syncBusyPoll          = 150 * time.Millisecond
)

func waitSyncNow(ctx context.Context, checkpoint bool, sp *tui.Status) error {
	done := make(chan error, 1)
	go func() {
		done <- waitSyncNowRPC(ctx, checkpoint)
	}()
	tick := time.NewTicker(syncBusyPoll)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			st, err := client.PeekStats()
			if err != nil || sp == nil {
				continue
			}
			if st.SyncStage != "" {
				sp.Set(st.SyncStage)
			}
		}
	}
}

func waitSyncNowRPC(ctx context.Context, checkpoint bool) error {
	ctx, cancel := context.WithTimeout(ctx, syncNowWait)
	defer cancel()
	return retrySyncNow(ctx, func() error {
		return client.SyncNow(protocol.SyncNowReq{Checkpoint: checkpoint})
	})
}

func retrySyncNow(ctx context.Context, syncNow func() error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := syncNow()
		if err == nil {
			return nil
		}
		if !isSyncAlreadyRunning(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(syncBusyPoll):
		}
	}
}

func isSyncAlreadyRunning(err error) bool {
	return err != nil && err.Error() == errSyncAlreadyRunning
}

func daemonControlUp() bool {
	_, err := client.PeekStats()
	return err == nil
}

func shouldLocalSyncFallback(rpcErr error, reachable bool) bool {
	if rpcErr == nil || isSyncAlreadyRunning(rpcErr) {
		return false
	}
	return !reachable
}

func kickSyncCheckpoint(ctx context.Context, a *app.App) error {
	if a == nil || !a.Config.Sync.IsEnabled() {
		return nil
	}
	if err := waitSyncNowRPC(ctx, true); err == nil {
		daemon.RecordOK()
		return nil
	} else if ctx.Err() != nil {
		return err
	} else if !shouldLocalSyncFallback(err, daemonControlUp()) {
		return err
	}
	if err := a.Sync(ctx, recoverySecretFromEnv(), tokensFromHardware(), nil); err != nil {
		return err
	}
	if err := a.ForceCheckpoint(ctx); err != nil {
		return err
	}
	daemon.RecordOK()
	return nil
}

func runSyncStatus(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "device: %s (%s)\n", a.Config.DeviceID, a.Config.DeviceName)
	fmt.Fprintf(out, "enabled: %v\n", a.Config.Sync.IsEnabled())
	fmt.Fprintf(out, "rclone_engine: %s\n", a.Config.Sync.RcloneEngineOrDefault())
	if len(a.Config.Sync.Endpoints) == 0 {
		fmt.Fprintln(out, "endpoints: none")
	}
	for _, ep := range a.Config.Sync.Endpoints {
		state := "disabled"
		if ep.Enabled {
			state = "enabled"
		}
		fmt.Fprintf(out, "endpoint %s type=%s %s", ep.ID, ep.Type, state)
		if ep.Provider != "" {
			fmt.Fprintf(out, " provider=%s", ep.Provider)
		}
		if ep.Path != "" {
			fmt.Fprintf(out, " path=%s", ep.Path)
		}
		fmt.Fprintln(out)
	}
	if !a.Config.Sync.IsEnabled() {
		fmt.Fprintln(out, "sync: disabled (local history only)")
		return nil
	}
	for _, ep := range a.Config.Sync.EnabledEndpoints() {
		tr, err := a.OpenTransport(ep)
		if err != nil {
			fmt.Fprintf(out, "%s transport error: %s\n", ep.ID, redact.String(err.Error()))
			continue
		}
		st, _ := tr.HealthCheck(cmd.Context())
		fmt.Fprintf(out, "%s auth: %s %s\n", ep.ID, st.State, redact.String(st.Message))
		rep, err := repository.Probe(cmd.Context(), tr)
		if err != nil {
			fmt.Fprintf(out, "%s probe: %s\n", ep.ID, redact.String(err.Error()))
		} else {
			fmt.Fprintf(out, "%s repository: %s %s\n", ep.ID, rep.Result, redact.String(rep.Message))
		}
		if _, err := tr.List(cmd.Context(), "metadata"); err != nil {
			fmt.Fprintf(out, "%s read: %s\n", ep.ID, redact.String(err.Error()))
		} else {
			fmt.Fprintf(out, "%s read: ok\n", ep.ID)
		}
	}
	f, err := merge.NewHeadStore(a.DB).Get()
	if err != nil {
		return err
	}
	for id, seq := range f {
		fmt.Fprintf(out, "head %s = %d\n", id, seq)
	}
	return nil
}

func runSyncDoctor(cmd *cobra.Command, _ []string) error {
	return runDoctor(cmd, nil)
}

func runSetup(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	res, err := setup.Run(cmd.Context(), a)
	if err != nil {
		return err
	}
	if res.Joined {
		fmt.Fprintln(cmd.OutOrStdout(), "joined existing history")
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), "setup complete")
	if res.RecoveryKey != "" {
		fmt.Fprintln(cmd.OutOrStdout(), "store this recovery key offline; it is not written to the remote:")
		fmt.Fprintln(cmd.OutOrStdout(), res.RecoveryKey)
	}
	return nil
}

func runDeviceList(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	devs, err := device.NewStore(a.DB).List()
	if err != nil {
		return err
	}
	for _, d := range devs {
		fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", d.ID, d.Name, d.Status)
	}
	return nil
}

func runDeviceAdd(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	if name != "" {
		a.Config.DeviceName = name
	}
	ranWizard := false
	if setup.NeedsJoinWizard(a) {
		if err := setup.ConfigureJoin(cmd.Context(), a); err != nil {
			return err
		}
		ranWizard = true
	} else if err := a.Config.Save(); err != nil {
		return err
	}
	if err := a.EnsureLocalDevice(); err != nil {
		return err
	}
	if !ranWizard {
		fmt.Fprintln(cmd.ErrOrStderr(), "checking configured endpoint for remnix metadata (not a Drive-wide scan)...")
		if err := setup.RequireValidRemote(cmd.Context(), a); err != nil {
			fmt.Fprintln(cmd.ErrOrStderr(), err)
			fmt.Fprintln(cmd.ErrOrStderr(), "re-running join wizard; pick the folder that already contains metadata/")
			if err := setup.ConfigureJoin(cmd.Context(), a); err != nil {
				return err
			}
		}
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "joining (sync)...")
	if err := a.Sync(cmd.Context(), recoverySecretFromEnv(), tokensFromHardware(), nil); err != nil {
		return err
	}
	daemon.RecordOK()
	fmt.Fprintln(cmd.ErrOrStderr(), "device joined; SMK stored in the OS keyring when unlock succeeded")
	offerDaemonInstall(cmd)
	return nil
}

func runDeviceRetire(cmd *cobra.Command, args []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	return a.RetireDevice(cmd.Context(), args[0], recoverySecretFromEnv(), tokensFromHardware(), nil)
}

func runDevicePrune(cmd *cobra.Command, args []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	return a.PruneDevice(cmd.Context(), args[0], recoverySecretFromEnv(), tokensFromHardware(), nil)
}

func runKeyStatus(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	gens, err := keys.NewStore(a.DB).Generations()
	if err != nil {
		return err
	}
	for _, g := range gens {
		state := "retained"
		if g.Active {
			state = "active"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "generation %s seq=%d %s slots=%d\n", g.GenerationID, g.Seq, state, len(g.Slots))
		for _, sl := range g.Slots {
			fmt.Fprintf(cmd.OutOrStdout(), "  slot %s type=%s status=%s\n", sl.ID, sl.Type, sl.Status)
		}
	}
	return nil
}

func runKeyGenerationList(cmd *cobra.Command, args []string) error {
	return runKeyStatus(cmd, args)
}

func runKeyYubiKeyAdd(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	tok, err := (piv.HardwareFactory{}).Generate("")
	if err != nil {
		toks, lerr := (piv.HardwareFactory{}).List()
		if lerr != nil || len(toks) == 0 {
			if infos, _ := fido2.List(); len(infos) > 0 {
				return fmt.Errorf("%w\n\nA FIDO2 security key is visible over USB HID but has no PIV applet. Enroll it with: remnix key fido add", err)
			}
			return err
		}
		tok = toks[0]
	}
	eng, err := a.Engine(recoverySecretFromEnv(), []piv.Token{tok}, nil)
	if err != nil {
		return err
	}
	smks, active, err := eng.Unlock()
	if err != nil {
		return err
	}
	smk := smks[active.GenerationID]
	updated, err := rotation.AddYubiKey(active, smk, tok)
	if err != nil {
		return err
	}
	return a.PublishGeneration(cmd.Context(), updated, smk, recoverySecretFromEnv(), []piv.Token{tok}, nil)
}

func runKeyYubiKeyRemove(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	eng, err := a.Engine(recoverySecretFromEnv(), tokensFromHardware(), nil)
	if err != nil {
		return err
	}
	smks, active, err := eng.Unlock()
	if err != nil {
		return err
	}
	var id string
	for _, sl := range active.Slots {
		if sl.Type == slots.TypeYubiKey && sl.Status == slots.StatusActive {
			id = sl.ID
		}
	}
	if id == "" {
		return fmt.Errorf("no active yubikey slot")
	}
	updated, err := rotation.RemoveSlot(active, smks[active.GenerationID], id)
	if err != nil {
		return err
	}
	return a.PublishGeneration(cmd.Context(), updated, smks[active.GenerationID], recoverySecretFromEnv(), tokensFromHardware(), nil)
}

func runKeyFIDOAdd(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	fmt.Fprintln(os.Stderr, "Touch the security key when it blinks (USB HID).")
	devs, err := fido2.OpenHMACDevices(fido2.PromptPIN)
	if err != nil {
		return err
	}
	defer fido2.CloseAll(devs)
	if len(devs) == 0 {
		return fmt.Errorf("no FIDO2 hmac-secret authenticator found over USB HID; check lsusb and hidraw access. Do not install pcscd for a Security Key")
	}
	eng, err := a.Engine(recoverySecretFromEnv(), tokensFromHardware(), devs)
	if err != nil {
		return err
	}
	smks, active, err := eng.Unlock()
	if err != nil {
		return err
	}
	smk := smks[active.GenerationID]
	updated, err := rotation.AddFIDO2(active, smk, devs[0])
	if err != nil {
		return err
	}
	return a.PublishGeneration(cmd.Context(), updated, smk, recoverySecretFromEnv(), tokensFromHardware(), devs)
}

func runKeyFIDORemove(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	eng, err := a.Engine(recoverySecretFromEnv(), tokensFromHardware(), nil)
	if err != nil {
		return err
	}
	smks, active, err := eng.Unlock()
	if err != nil {
		return err
	}
	var id string
	for _, sl := range active.Slots {
		if sl.Type == slots.TypeFIDO2Hmac && sl.Status == slots.StatusActive {
			id = sl.ID
		}
	}
	if id == "" {
		return fmt.Errorf("no active fido2-hmac slot")
	}
	updated, err := rotation.RemoveSlot(active, smks[active.GenerationID], id)
	if err != nil {
		return err
	}
	return a.PublishGeneration(cmd.Context(), updated, smks[active.GenerationID], recoverySecretFromEnv(), tokensFromHardware(), nil)
}

func runKeyRecoveryRotate(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	eng, err := a.Engine(recoverySecretFromEnv(), tokensFromHardware(), nil)
	if err != nil {
		return err
	}
	smks, active, err := eng.Unlock()
	if err != nil {
		return err
	}
	updated, _, encoded, err := rotation.RotateRecovery(active, smks[active.GenerationID])
	if err != nil {
		return err
	}
	if err := a.PublishGeneration(cmd.Context(), updated, smks[active.GenerationID], recoverySecretFromEnv(), tokensFromHardware(), nil); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), encoded)
	return nil
}

func runKeyRecover(cmd *cobra.Command, args []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	var generationID string
	if len(args) == 1 {
		generationID = args[0]
	}
	fmt.Fprintln(os.Stderr, "Unlock the generation to recover (recovery key, YubiKey, or FIDO touch).")
	fidoDevs, err := fido2.OpenHMACDevices(fido2.PromptPIN)
	if err != nil {
		fidoDevs = nil
	}
	defer fido2.CloseAll(fidoDevs)
	active, err := a.RecoverGeneration(cmd.Context(), generationID, recoverySecretFromEnv(), tokensFromHardware(), fidoDevs)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "recovered generation %s seq=%d; history restored from checkpoint when one was present\n", active.GenerationID, active.Seq)
	return nil
}

func runKeyRotate(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	secret := recoverySecretFromEnv()
	tokens := tokensFromHardware()
	fidoDevs, err := fido2.OpenHMACDevices(fido2.PromptPIN)
	if err != nil {
		fidoDevs = nil
	}
	defer fido2.CloseAll(fidoDevs)
	eng, err := a.Engine(secret, tokens, fidoDevs)
	if err != nil {
		return err
	}
	_, active, err := eng.Unlock()
	if err != nil {
		return err
	}
	next, smk, encoded, err := rotation.NewGenerationFromSlots(active, secret, tokens, fidoDevs)
	if err != nil {
		return err
	}
	active.Active = false
	if err := keys.NewStore(a.DB).PutGeneration(active); err != nil {
		return err
	}
	if err := a.PublishGeneration(cmd.Context(), next, smk, secret, tokens, fidoDevs); err != nil {
		return err
	}
	if encoded != "" {
		fmt.Fprintln(cmd.OutOrStdout(), encoded)
	}
	return nil
}

func runGC(cmd *cobra.Command, dryRun bool) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	plan, err := a.GarbageCollect(cmd.Context(), dryRun)
	if err != nil {
		return err
	}
	b, _ := gc.EncodePlan(plan)
	fmt.Fprintln(cmd.OutOrStdout(), string(b))
	return nil
}

func runGCStatus(cmd *cobra.Command, _ []string) error {
	return runGC(cmd, true)
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	return doctor.Run(cmd.Context(), a, cmd.OutOrStdout())
}

func runUnlock(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	eng, err := a.Engine(recoverySecretFromEnv(), tokensFromHardware(), nil)
	if err != nil {
		return err
	}
	smks, active, err := eng.Unlock()
	if err != nil {
		return err
	}
	if err := keyring.Set(a.Config.DeviceID, smks); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "unlocked generation %s; SMK stored in the OS keyring\n", active.GenerationID)
	return nil
}

func runDaemon(cmd *cobra.Command, _ []string) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return daemon.Run(ctx)
}

func runDaemonInstall(cmd *cobra.Command, _ []string) error {
	if err := daemon.Install(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "daemon installed for this user session")
	return nil
}

func runDaemonUninstall(cmd *cobra.Command, _ []string) error {
	if err := daemon.Uninstall(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "daemon autostart removed")
	return nil
}

func printDaemonStats(cmd *cobra.Command, st protocol.Stats) {
	fmt.Fprintf(cmd.OutOrStdout(), "pid=%d uptime=%ds heap=%d rss=%d sessions=%d ptys=%d cache_cmds=%d cache_interned=%d cache_bytes=%d dirty=%v db_open=%d last_sync_ok=%v %s\n",
		st.PID, st.UptimeSec, st.HeapAlloc, st.RSSBytes, st.Sessions, st.ActivePTYs, st.CacheEntries, st.CacheInterned, st.CacheBytes, st.CacheDirty, st.DBOpenConns, st.LastSyncOK, formatLiveSync(st))
}

func runDaemonStats(cmd *cobra.Command, _ []string) error {
	st, err := client.Stats()
	if err != nil {
		return err
	}
	printDaemonStats(cmd, st)
	return nil
}

func runDaemonReload(cmd *cobra.Command, _ []string) error {
	if err := client.ReloadConfig(); err != nil {
		return fmt.Errorf("daemon not running: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "daemon config reloaded")
	return nil
}

func runDaemonCompact(cmd *cobra.Command, _ []string) error {
	st, err := client.CompactCache()
	if err != nil {
		return fmt.Errorf("daemon not running: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), "cache compacted")
	printDaemonStats(cmd, st)
	return nil
}

func runDaemonRestart(cmd *cobra.Command, _ []string) error {
	if err := daemon.Restart(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "daemon restarted")
	return nil
}

func runDaemonStatus(cmd *cobra.Command, _ []string) error {
	watch, _ := cmd.Flags().GetBool("watch")
	if watch {
		return watchDaemonStatus(cmd)
	}
	lines, err := daemonStatusLines()
	if err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Fprintln(cmd.OutOrStdout(), line)
	}
	return nil
}

func daemonStatusLines() ([]string, error) {
	st, err := daemon.Query()
	if err != nil {
		return nil, err
	}
	lines := []string{fmt.Sprintf("installed=%v running=%v path=%s %s", st.Installed, st.Running, st.Path, st.Detail)}
	last, err := daemon.ReadStatus()
	if err != nil {
		return nil, err
	}
	if last.At != 0 {
		when := last.HumanAt
		if when == "" {
			when = fmt.Sprintf("%d", last.At)
		}
		lines = append(lines, fmt.Sprintf("last_ok=%v at=%s%s gc_deleted=%d gc_eligible=%v err=%s", last.OK, when, last.FormatTiming(), last.GCDeleted, last.GCEligible, last.Error))
		if !last.OK && strings.Contains(last.Error, "context canceled") {
			lines = append(lines, "hint: that error is from a stopped/restarted sync, not necessarily the last successful join; run: systemctl --user restart remnix-daemon")
		}
	}
	if live, err := client.PeekStats(); err == nil {
		lines = append(lines, formatLiveSync(live))
	} else if st.Running {
		lines = append(lines, "sync=unreachable")
	}
	return lines, nil
}

func formatLiveSync(st protocol.Stats) string {
	if !st.SyncRunning {
		return "sync=idle"
	}
	stage := st.SyncStage
	if stage == "" {
		stage = "working"
	}
	if s := daemon.FormatElapsed(st.SyncElapsedMs); s != "" {
		return fmt.Sprintf("sync=running stage=%q elapsed=%s", stage, s)
	}
	return fmt.Sprintf("sync=running stage=%q", stage)
}

func watchDaemonStatus(cmd *cobra.Command) error {
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	out := cmd.OutOrStdout()
	f, ok := out.(*os.File)
	tty := ok && term.IsTerminal(int(f.Fd()))
	if tty {
		return watchDaemonStatusTTY(ctx, cmd)
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		lines, err := daemonStatusLines()
		if err != nil {
			return err
		}
		for _, line := range lines {
			fmt.Fprintln(out, line)
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return ctx.Err()
		case <-tick.C:
		}
	}
}

func watchDaemonStatusTTY(ctx context.Context, cmd *cobra.Command) error {
	header, err := daemonStatusLines()
	if err != nil {
		return err
	}
	live := ""
	if n := len(header); n > 0 && strings.HasPrefix(header[n-1], "sync=") {
		live = header[n-1]
		header = header[:n-1]
	}
	for _, line := range header {
		fmt.Fprintln(cmd.OutOrStdout(), line)
	}
	sp := tui.StartStatus(cmd.ErrOrStderr())
	if live == "" {
		live = "sync=idle"
	}
	sp.Set(live)
	defer sp.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil
			}
			return ctx.Err()
		case <-tick.C:
			if live, err := client.PeekStats(); err == nil {
				sp.Set(formatLiveSync(live))
			} else {
				sp.Set("sync=unreachable")
			}
		}
	}
}

func offerDaemonInstall(cmd *cobra.Command) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(cmd.ErrOrStderr(), "to sync at login: remnix daemon install")
		return
	}
	startDaemon := true
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Start sync at login on this device?").Value(&startDaemon),
	))
	if err := form.RunWithContext(cmd.Context()); err != nil || !startDaemon {
		return
	}
	if err := daemon.Install(); err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "could not install login daemon:", err)
		fmt.Fprintln(cmd.ErrOrStderr(), "you can retry with: remnix daemon install")
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), "daemon installed for this user session")
}
