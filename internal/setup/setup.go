package setup

import (
	"context"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/mistweaverco/syncsh/internal/app"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
	"github.com/mistweaverco/syncsh/internal/crypto/keys"
	"github.com/mistweaverco/syncsh/internal/crypto/piv"
	"github.com/mistweaverco/syncsh/internal/crypto/recovery"
	"github.com/mistweaverco/syncsh/internal/crypto/rotation"
	"github.com/mistweaverco/syncsh/internal/daemon"
	"github.com/mistweaverco/syncsh/internal/device"
	"github.com/mistweaverco/syncsh/internal/sync/syncer"
)

type Result struct {
	RecoveryKey string
}

func Run(ctx context.Context, a *app.App) (Result, error) {
	store := keys.NewStore(a.DB)
	if err := store.ClearUnpublished(); err != nil {
		return Result{}, err
	}
	if _, ok, err := store.Active(); err != nil {
		return Result{}, err
	} else if ok {
		return Result{}, fmt.Errorf("this device is already set up; use 'syncsh key fido add' or 'syncsh key yubikey add' to enroll a hardware key, or 'syncsh key status' to inspect")
	}

	cfg := a.Config
	if cfg.DeviceName == "" {
		cfg.DeviceName = device.DefaultName()
	}
	transport := cfg.Sync.Transport
	if transport == "" {
		transport = "directory"
	}
	dirPath := cfg.Sync.Directory.Path
	enrollHW := false

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Device name").Value(&cfg.DeviceName),
			huh.NewSelect[string]().
				Title("Sync transport").
				Options(
					huh.NewOption("Directory (Dropbox, Drive, Syncthing, …)", "directory"),
					huh.NewOption("rsync", "rsync"),
					huh.NewOption("scp", "scp"),
				).
				Value(&transport),
			huh.NewInput().Title("Directory remote path").Value(&dirPath),
			huh.NewConfirm().Title("Enroll a hardware key").Value(&enrollHW),
		),
	)
	if err := form.RunWithContext(ctx); err != nil {
		return Result{}, err
	}
	cfg.Sync.Transport = transport
	cfg.Sync.Directory.Path = dirPath
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
	if err := a.EnsureLocalDevice(); err != nil {
		return Result{}, err
	}
	if err := refuseInitializedRemote(ctx, a); err != nil {
		return Result{}, err
	}

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
	eng, err := a.Engine(secret, tokens, fidoDevs)
	if err != nil {
		return Result{}, err
	}
	if err := eng.InitializeRemote(ctx, m, smk); err != nil {
		return Result{}, err
	}
	if err := keyring.Set(a.Config.DeviceID, map[string][]byte{m.GenerationID: smk}); err != nil {
		fmt.Fprintln(os.Stderr, "could not store SMK in OS keyring:", err)
		fmt.Fprintln(os.Stderr, "run syncsh unlock after the keyring is available")
	}
	startDaemon := true
	dform := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Start sync at login on this device?").Value(&startDaemon),
	))
	if err := dform.RunWithContext(ctx); err != nil {
		return Result{RecoveryKey: encoded}, err
	}
	if startDaemon {
		if err := daemon.Install(); err != nil {
			fmt.Fprintln(os.Stderr, "could not install login daemon:", err)
			fmt.Fprintln(os.Stderr, "you can retry with: syncsh daemon install")
		}
	}
	return Result{RecoveryKey: encoded}, nil
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
	a.Config.Sync.Transport = "directory"
	a.Config.Sync.Directory.Path = dirPath
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
	return Result{RecoveryKey: encoded}, nil
}

func refuseInitializedRemote(ctx context.Context, a *app.App) error {
	tr, err := a.Transport()
	if err != nil {
		return err
	}
	exists, err := syncer.RemoteInitialized(ctx, tr)
	if err != nil {
		return err
	}
	if exists {
		return syncer.ErrRemoteInitialized
	}
	return nil
}
