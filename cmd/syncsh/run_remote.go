package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"charm.land/huh/v2"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
	"github.com/mistweaverco/syncsh/internal/crypto/keys"
	"github.com/mistweaverco/syncsh/internal/crypto/piv"
	"github.com/mistweaverco/syncsh/internal/crypto/recovery"
	"github.com/mistweaverco/syncsh/internal/crypto/rotation"
	"github.com/mistweaverco/syncsh/internal/crypto/slots"
	"github.com/mistweaverco/syncsh/internal/daemon"
	"github.com/mistweaverco/syncsh/internal/device"
	"github.com/mistweaverco/syncsh/internal/doctor"
	"github.com/mistweaverco/syncsh/internal/setup"
	"github.com/mistweaverco/syncsh/internal/sync/gc"
	"github.com/mistweaverco/syncsh/internal/sync/merge"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func recoverySecretFromEnv() []byte {
	if env := os.Getenv("SYNCSH_RECOVERY_KEY"); env != "" {
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

func runSync(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	return a.Sync(cmd.Context(), recoverySecretFromEnv(), tokensFromHardware(), nil)
}

func runSyncStatus(cmd *cobra.Command, _ []string) error {
	a, err := openApp()
	if err != nil {
		return err
	}
	defer a.Close()
	f, err := merge.NewHeadStore(a.DB).Get()
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "device: %s (%s)\n", a.Config.DeviceID, a.Config.DeviceName)
	fmt.Fprintf(cmd.OutOrStdout(), "transport: %s\n", a.Config.Sync.Transport)
	for id, seq := range f {
		fmt.Fprintf(cmd.OutOrStdout(), "head %s = %d\n", id, seq)
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
	fmt.Fprintln(cmd.OutOrStdout(), "setup complete")
	fmt.Fprintln(cmd.OutOrStdout(), "store this recovery key offline; it is not written to the remote:")
	fmt.Fprintln(cmd.OutOrStdout(), res.RecoveryKey)
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
		if err := a.Config.Save(); err != nil {
			return err
		}
	}
	if err := a.EnsureLocalDevice(); err != nil {
		return err
	}
	if err := a.Sync(cmd.Context(), recoverySecretFromEnv(), tokensFromHardware(), nil); err != nil {
		return err
	}
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
				return fmt.Errorf("%w\n\nA FIDO2 security key is visible over USB HID but has no PIV applet. Enroll it with: syncsh key fido add", err)
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
	return eng.PublishGeneration(cmd.Context(), updated, smk)
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
	return eng.PublishGeneration(cmd.Context(), updated, smks[active.GenerationID])
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
	return eng.PublishGeneration(cmd.Context(), updated, smk)
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
	return eng.PublishGeneration(cmd.Context(), updated, smks[active.GenerationID])
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
	if err := eng.PublishGeneration(cmd.Context(), updated, smks[active.GenerationID]); err != nil {
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
	eng, err := a.Engine(recoverySecretFromEnv(), tokensFromHardware(), fidoDevs)
	if err != nil {
		return err
	}
	active, err := eng.RecoverGeneration(cmd.Context(), generationID)
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
	if err := eng.PublishGeneration(cmd.Context(), next, smk); err != nil {
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
	fmt.Fprintln(cmd.OutOrStdout(), "sync daemon installed for this user session")
	return nil
}

func runDaemonUninstall(cmd *cobra.Command, _ []string) error {
	if err := daemon.Uninstall(); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "sync daemon autostart removed")
	return nil
}

func runDaemonStatus(cmd *cobra.Command, _ []string) error {
	st, err := daemon.Query()
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "installed=%v running=%v path=%s %s\n", st.Installed, st.Running, st.Path, st.Detail)
	last, err := daemon.ReadStatus()
	if err != nil {
		return err
	}
	if last.At != 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "last_ok=%v at=%d gc_deleted=%d gc_eligible=%v err=%s\n", last.OK, last.At, last.GCDeleted, last.GCEligible, last.Error)
	}
	return nil
}

func offerDaemonInstall(cmd *cobra.Command) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(cmd.ErrOrStderr(), "to sync at login: syncsh daemon install")
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
		fmt.Fprintln(cmd.ErrOrStderr(), "you can retry with: syncsh daemon install")
		return
	}
	fmt.Fprintln(cmd.OutOrStdout(), "sync daemon installed for this user session")
}
