package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mistweaverco/syncsh/internal/app"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
	"github.com/mistweaverco/syncsh/internal/crypto/keys"
	"github.com/mistweaverco/syncsh/internal/crypto/slots"
	"github.com/mistweaverco/syncsh/internal/sync/syncer"
	"github.com/mistweaverco/syncsh/internal/transport/directory"
)

func TestMain(m *testing.M) {
	keyring.Use(keyring.NewMemory())
	os.Exit(m.Run())
}

func TestRunNonInteractiveIdempotentError(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(root, "cfg"))
	t.Setenv("SYNCSH_DATA_DIR", filepath.Join(root, "data"))
	a, err := app.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	remote := filepath.Join(root, "remote")
	if _, err := RunNonInteractive(context.Background(), a, remote, "test", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := RunNonInteractive(context.Background(), a, remote, "test", nil, nil); err == nil {
		t.Fatal("expected already-set-up error")
	}
}

func TestRunNonInteractiveRefusesExistingRemote(t *testing.T) {
	root := t.TempDir()
	remote := filepath.Join(root, "remote")
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(root, "cfg-a"))
	t.Setenv("SYNCSH_DATA_DIR", filepath.Join(root, "data-a"))
	a, err := app.Open()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunNonInteractive(context.Background(), a, remote, "a", nil, nil); err != nil {
		t.Fatal(err)
	}
	a.Close()

	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(root, "cfg-b"))
	t.Setenv("SYNCSH_DATA_DIR", filepath.Join(root, "data-b"))
	b, err := app.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := RunNonInteractive(context.Background(), b, remote, "b", nil, nil); !errors.Is(err, syncer.ErrRemoteInitialized) {
		t.Fatalf("got %v", err)
	}
}

func TestRunNonInteractiveStoresSMKInKeyring(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(root, "cfg"))
	t.Setenv("SYNCSH_DATA_DIR", filepath.Join(root, "data"))
	a, err := app.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := RunNonInteractive(context.Background(), a, filepath.Join(root, "remote"), "test", nil, nil); err != nil {
		t.Fatal(err)
	}
	smks, err := keyring.Get(a.Config.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(smks) == 0 {
		t.Fatal("expected SMK in keyring after setup")
	}
}

func TestRunNonInteractiveWithFakeFIDO2(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(root, "cfg"))
	t.Setenv("SYNCSH_DATA_DIR", filepath.Join(root, "data"))
	a, err := app.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	dev := fido2.NewFakeDevice("security-key")
	if _, err := RunNonInteractive(context.Background(), a, filepath.Join(root, "remote"), "test", nil, []fido2.Device{dev}); err != nil {
		t.Fatal(err)
	}
	m, ok, err := keys.NewStore(a.DB).Active()
	if err != nil || !ok {
		t.Fatalf("active: ok=%v err=%v", ok, err)
	}
	fidoSlots := slots.OfType(m.Slots, slots.TypeFIDO2Hmac)
	if len(fidoSlots) != 1 {
		t.Fatalf("expected one fido2-hmac slot, got %d", len(fidoSlots))
	}
	if _, sl, err := keys.UnwrapAny(m.Slots, keys.Unlock{FIDO2: []fido2.Device{dev}}); err != nil || sl.Type != slots.TypeFIDO2Hmac {
		t.Fatalf("fido unwrap: %v %+v", err, sl)
	}
}

func TestCheckpointAndGarbageCollect(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SYNCSH_CONFIG_DIR", filepath.Join(root, "cfg"))
	t.Setenv("SYNCSH_DATA_DIR", filepath.Join(root, "data"))
	a, err := app.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	remote := filepath.Join(root, "remote")
	if _, err := RunNonInteractive(context.Background(), a, remote, "test", nil, nil); err != nil {
		t.Fatal(err)
	}
	a.Config.Sync.GCInterval = "1ns"
	if err := a.Sync(context.Background(), nil, nil, []fido2.Device{}); err != nil {
		t.Fatal(err)
	}
	if err := a.MaybeCheckpoint(context.Background()); err != nil {
		t.Fatal(err)
	}
	plan, err := a.GarbageCollect(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Eligible {
		t.Fatalf("gc not eligible: %+v", plan)
	}
	tr := directory.New(remote)
	objs, err := tr.List(context.Background(), "checkpoints/")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) == 0 {
		t.Fatal("expected a checkpoint on the remote")
	}
}
