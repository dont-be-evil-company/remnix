package setup

import (
	"context"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/dont-be-evil-company/remnix/internal/app"
	"github.com/dont-be-evil-company/remnix/internal/config"
	"github.com/dont-be-evil-company/remnix/internal/crypto/fido2"
	"github.com/dont-be-evil-company/remnix/internal/crypto/keyring"
	"github.com/dont-be-evil-company/remnix/internal/crypto/keys"
	"github.com/dont-be-evil-company/remnix/internal/crypto/piv"
	"github.com/dont-be-evil-company/remnix/internal/crypto/recovery"
	"github.com/dont-be-evil-company/remnix/internal/crypto/rotation"
	"github.com/dont-be-evil-company/remnix/internal/daemon"
	"github.com/dont-be-evil-company/remnix/internal/device"
	"github.com/dont-be-evil-company/remnix/internal/repository"
	"github.com/dont-be-evil-company/remnix/internal/sync/syncer"
	"github.com/dont-be-evil-company/remnix/internal/tui/wizard"
)

type Result struct {
	RecoveryKey string
	Joined      bool
}

func Run(ctx context.Context, a *app.App) (Result, error) {
	st, err := LoadState()
	if err != nil {
		return Result{}, err
	}
	if st != nil && st.Phase != "" {
		fmt.Fprintln(os.Stderr, "resuming interrupted setup (delete", "setup-state.json", "to start over)")
		return resume(ctx, a, st)
	}

	store := keys.NewStore(a.DB)
	if err := store.ClearUnpublished(); err != nil {
		return Result{}, err
	}
	if _, ok, err := store.Active(); err != nil {
		return Result{}, err
	} else if ok {
		return Result{}, fmt.Errorf("this device is already set up; use 'remnix key fido add' or 'remnix key yubikey add' to enroll a hardware key, or 'remnix key status' to inspect")
	}

	cfg := a.Config
	if cfg.DeviceName == "" {
		cfg.DeviceName = device.DefaultName()
	}
	enrollHW := false
	intent, err := wizard.ChooseIntent(ctx, wizard.IntentCreate)
	if err != nil {
		return Result{}, err
	}
	if intent == wizard.IntentExit {
		return Result{}, fmt.Errorf("setup cancelled")
	}
	if intent == wizard.IntentJoin {
		if err := ConfigureJoin(ctx, a); err != nil {
			return Result{}, err
		}
		if err := a.EnsureLocalDevice(); err != nil {
			return Result{}, err
		}
		if err := a.Sync(ctx, recoverySecretFromEnv(), tokensFromHardware(), nil); err != nil {
			return Result{}, err
		}
		offerDaemon(ctx)
		return Result{Joined: true}, nil
	}
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Device name").Value(&cfg.DeviceName),
		huh.NewConfirm().Title("Enroll a hardware key").Value(&enrollHW),
	)).RunWithContext(ctx); err != nil {
		return Result{}, err
	}
	switch intent {
	case wizard.IntentLocal:
		off := false
		cfg.Sync.Enabled = &off
		cfg.Sync.Endpoints = nil
	default:
		ep, err := wizard.AddEndpoint(ctx, cfg, false)
		if err != nil {
			return Result{}, err
		}
		if ep.ID == "" {
			off := false
			cfg.Sync.Enabled = &off
		} else {
			on := true
			cfg.Sync.Enabled = &on
			cfg.Sync.UpsertEndpoint(ep)
		}
	}
	if cfg.DeviceID == "" {
		id, err := device.NewID()
		if err != nil {
			return Result{}, err
		}
		cfg.DeviceID = id
	}
	if err := cfg.Save(); err != nil {
		return Result{}, err
	}
	a.Config = cfg
	marker := &State{Phase: PhaseConfig, DeviceID: cfg.DeviceID, DeviceName: cfg.DeviceName}
	if err := marker.Save(); err != nil {
		return Result{}, err
	}
	if err := a.EnsureLocalDevice(); err != nil {
		return Result{}, err
	}
	if cfg.Sync.IsEnabled() {
		if err := refuseInitializedRemote(ctx, a); err != nil {
			return Result{}, err
		}
	}
	return finishCreate(ctx, a, enrollHW, marker)
}

func resume(ctx context.Context, a *app.App, st *State) (Result, error) {
	if st.DeviceID != "" {
		a.Config.DeviceID = st.DeviceID
	}
	if st.DeviceName != "" && a.Config.DeviceName == "" {
		a.Config.DeviceName = st.DeviceName
	}
	store := keys.NewStore(a.DB)
	if st.Phase == PhaseKeys || st.Phase == PhaseRemote {
		m, ok, err := store.Active()
		if err != nil {
			return Result{}, err
		}
		if !ok {
			_ = ClearState()
			return Result{}, fmt.Errorf("partial setup is missing keys; delete setup-state.json and run setup again")
		}
		smks, _ := keyring.Get(a.Config.DeviceID)
		smk := smks[m.GenerationID]
		if len(smk) == 0 {
			return Result{}, fmt.Errorf("partial setup: SMK is not in the keyring; run remnix unlock, then retry setup")
		}
		if a.Config.Sync.IsEnabled() {
			eng, err := a.Engine(nil, nil, nil)
			if err != nil {
				return Result{}, err
			}
			if err := eng.InitializeRemote(ctx, m, smk); err != nil {
				return Result{}, err
			}
		}
		_ = ClearState()
		offerDaemon(ctx)
		return Result{}, nil
	}
	return finishCreate(ctx, a, false, st)
}

func finishCreate(ctx context.Context, a *app.App, enrollHW bool, marker *State) (Result, error) {
	fidoDevs, tokens, err := enrollHardware(enrollHW)
	if err != nil {
		return Result{}, err
	}
	defer fido2.CloseAll(fidoDevs)

	m, smk, encoded, err := rotation.BootstrapGeneration(1, nil, tokens, fidoDevs)
	if err != nil {
		return Result{}, err
	}
	secret, err := recovery.Decode(encoded)
	if err != nil {
		return Result{}, err
	}
	if _, _, err := keys.UnwrapAny(m.Slots, keys.Unlock{RecoverySecret: secret}); err != nil {
		return Result{}, fmt.Errorf("recovery key failed verification: %w", err)
	}
	for _, tok := range tokens {
		if _, _, err := keys.UnwrapAny(m.Slots, keys.Unlock{Tokens: []piv.Token{tok}}); err != nil {
			return Result{}, fmt.Errorf("yubikey failed verification: %w", err)
		}
	}
	for _, d := range fidoDevs {
		fmt.Fprintln(os.Stderr, "Touch the security key again to verify unlock.")
		if _, _, err := keys.UnwrapAny(m.Slots, keys.Unlock{FIDO2: []fido2.Device{d}}); err != nil {
			return Result{}, fmt.Errorf("fido2 key failed verification: %w", err)
		}
	}
	if err := keys.NewStore(a.DB).PutGeneration(m); err != nil {
		return Result{}, err
	}
	if err := keyring.Set(a.Config.DeviceID, map[string][]byte{m.GenerationID: smk}); err != nil {
		fmt.Fprintln(os.Stderr, "could not store SMK in OS keyring:", err)
		fmt.Fprintln(os.Stderr, "run remnix unlock after the keyring is available")
	}
	if marker != nil {
		marker.Phase = PhaseKeys
		marker.GenerationID = m.GenerationID
		_ = marker.Save()
	}
	if a.Config.Sync.IsEnabled() {
		eng, err := a.Engine(secret, tokens, fidoDevs)
		if err != nil {
			return Result{}, err
		}
		if err := eng.InitializeRemote(ctx, m, smk); err != nil {
			return Result{}, err
		}
	}
	_ = ClearState()
	offerDaemon(ctx)
	return Result{RecoveryKey: encoded}, nil
}

func offerDaemon(ctx context.Context) {
	startDaemon := true
	dform := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Start sync at login on this device?").Value(&startDaemon),
	))
	if err := dform.RunWithContext(ctx); err != nil || !startDaemon {
		return
	}
	if err := daemon.Install(); err != nil {
		fmt.Fprintln(os.Stderr, "could not install login daemon:", err)
		fmt.Fprintln(os.Stderr, "you can retry with: remnix daemon install")
	}
}

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

func enrollHardware(want bool) ([]fido2.Device, []piv.Token, error) {
	if !want {
		return nil, nil, nil
	}
	fmt.Fprintln(os.Stderr, "Touch the security key when it blinks (USB HID; NFC is not used).")
	fidoDevs, err := fido2.OpenHMACDevices(fido2.PromptPIN)
	if err != nil {
		return nil, nil, err
	}
	if len(fidoDevs) > 0 {
		return fidoDevs, nil, nil
	}
	tokens, err := enrollYubiKeys()
	if err != nil {
		return nil, nil, fmt.Errorf("%w\n\nFIDO-only Security Keys have no PIV applet. Plug the key in over USB and check hidraw access (lsusb). Do not install pcscd for that device; use FIDO2 hmac-secret instead", err)
	}
	return nil, tokens, nil
}

func enrollYubiKeys() ([]piv.Token, error) {
	toks, err := (piv.HardwareFactory{}).List()
	if err != nil {
		return nil, fmt.Errorf("could not access YubiKey PIV: %w", err)
	}
	if len(toks) > 0 {
		return toks, nil
	}
	fmt.Fprintln(os.Stderr, "YubiKey found but PIV slot 9d is empty; generating a key on the token")
	tok, err := (piv.HardwareFactory{}).Generate("")
	if err != nil {
		return nil, fmt.Errorf("could not provision YubiKey PIV slot: %w", err)
	}
	return []piv.Token{tok}, nil
}

func RunNonInteractive(ctx context.Context, a *app.App, dirPath, name string, fakeTokens []piv.Token, fakeFIDO []fido2.Device) (Result, error) {
	store := keys.NewStore(a.DB)
	if err := store.ClearUnpublished(); err != nil {
		return Result{}, err
	}
	if _, ok, err := store.Active(); err != nil {
		return Result{}, err
	} else if ok {
		return Result{}, fmt.Errorf("this device is already set up")
	}
	if a.Config.DeviceID == "" {
		id, err := device.NewID()
		if err != nil {
			return Result{}, err
		}
		a.Config.DeviceID = id
	}
	if name == "" {
		name = device.DefaultName()
	}
	a.Config.DeviceName = name
	on := true
	a.Config.Sync.Enabled = &on
	a.Config.Sync.UpsertEndpoint(config.DirectoryEndpoint("local", dirPath))
	if err := a.Config.Save(); err != nil {
		return Result{}, err
	}
	if err := a.EnsureLocalDevice(); err != nil {
		return Result{}, err
	}
	if err := refuseInitializedRemote(ctx, a); err != nil {
		return Result{}, err
	}
	m, smk, encoded, err := rotation.BootstrapGeneration(1, nil, fakeTokens, fakeFIDO)
	if err != nil {
		return Result{}, err
	}
	secret, err := recovery.Decode(encoded)
	if err != nil {
		return Result{}, err
	}
	eng, err := a.Engine(secret, fakeTokens, fakeFIDO)
	if err != nil {
		return Result{}, err
	}
	if err := eng.InitializeRemote(ctx, m, smk); err != nil {
		return Result{}, err
	}
	_ = keyring.Set(a.Config.DeviceID, map[string][]byte{m.GenerationID: smk})
	_ = ClearState()
	return Result{RecoveryKey: encoded}, nil
}

func refuseInitializedRemote(ctx context.Context, a *app.App) error {
	eps := a.Config.Sync.EnabledEndpoints()
	if len(eps) == 0 {
		return nil
	}
	tr, err := a.OpenTransport(eps[0])
	if err != nil {
		return err
	}
	rep, err := repository.Probe(ctx, tr)
	if err != nil {
		return err
	}
	switch rep.Result {
	case repository.Valid:
		return syncer.ErrRemoteInitialized
	case repository.Partial, repository.UnsupportedVersion:
		return fmt.Errorf("%w (%s)", syncer.ErrRemoteInitialized, rep.Message)
	}
	return nil
}
