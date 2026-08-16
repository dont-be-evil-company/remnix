package doctor

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/mistweaverco/syncsh/internal/app"
	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/keyring"
	"github.com/mistweaverco/syncsh/internal/crypto/keys"
	"github.com/mistweaverco/syncsh/internal/crypto/piv"
	"github.com/mistweaverco/syncsh/internal/crypto/slots"
	"github.com/mistweaverco/syncsh/internal/daemon"
	"github.com/mistweaverco/syncsh/internal/db"
	"github.com/mistweaverco/syncsh/internal/device"
	"github.com/mistweaverco/syncsh/internal/sync/gc"
	"github.com/mistweaverco/syncsh/internal/sync/merge"
)

type Report struct {
	OK       bool
	Findings []string
}

func Run(ctx context.Context, a *app.App, w io.Writer) error {
	r := Report{OK: true}
	check := func(ok bool, msg string) {
		fmt.Fprintln(w, msg)
		if !ok {
			r.OK = false
			r.Findings = append(r.Findings, msg)
		}
	}

	check(a.Config.Version == config.CurrentVersion, fmt.Sprintf("config version: %d", a.Config.Version))
	check(a.Config.DeviceID != "", "device id: "+a.Config.DeviceID)
	if a.Config.Database.Path != "" {
		check(true, "database path: "+a.Config.Database.Path)
	}
	res, err := db.IntegrityCheck(a.DB.SQL)
	check(err == nil && res == "ok", "sqlite integrity: "+res)
	st, err := db.MigrationStatus(a.DB.SQL)
	if err != nil {
		check(false, "migrations: "+err.Error())
	} else {
		for _, s := range st {
			if !s.Applied || s.Mismatch {
				check(false, "migration issue: "+s.ID)
			}
		}
		check(true, fmt.Sprintf("migrations: %d applied", len(st)))
	}

	devs, _ := device.NewStore(a.DB).List()
	check(len(devs) > 0, fmt.Sprintf("devices: %d", len(devs)))
	ks := keys.NewStore(a.DB)
	gens, _ := ks.Generations()
	check(len(gens) > 0, fmt.Sprintf("key generations: %d", len(gens)))
	hasFIDOSlot := false
	for _, g := range gens {
		if len(slots.OfType(g.Slots, slots.TypeFIDO2Hmac)) > 0 {
			hasFIDOSlot = true
		}
	}
	reportHardware(check, hasFIDOSlot)
	reportKeyringAndDaemon(check, a.Config.DeviceID)

	if _, err := a.Transport(); err != nil {
		check(false, "transport: "+err.Error())
	} else {
		check(true, "transport: "+a.Config.Sync.Transport)
		tr, _ := a.Transport()
		objs, err := tr.List(ctx, "")
		if err != nil {
			check(false, "remote list: "+err.Error())
		} else {
			check(true, fmt.Sprintf("remote objects: %d", len(objs)))
		}
		plan, err := gc.Evaluate(ctx, tr, gc.RequiredDevices(devs), "")
		if err == nil {
			check(true, fmt.Sprintf("gc eligible: %v blocked_by=%v", plan.Eligible, plan.BlockedBy))
		}
	}
	f, _ := merge.NewHeadStore(a.DB).Get()
	check(true, fmt.Sprintf("local frontier: %d devices", len(f)))
	host, _ := os.Hostname()
	check(host != "", "hostname: "+host)
	if !r.OK {
		return fmt.Errorf("doctor found %d issue(s)", len(r.Findings))
	}
	fmt.Fprintln(w, "doctor: ok")
	return nil
}

func reportHardware(check func(bool, string), hasFIDOSlot bool) {
	infos, err := fido2.List()
	if err != nil {
		check(!hasFIDOSlot, "fido2 hid: "+err.Error())
	} else {
		check(true, fmt.Sprintf("fido2 hid devices: %d", len(infos)))
		hmac := 0
		for _, inf := range infos {
			cap := "no hmac-secret"
			if inf.HMACSecret {
				hmac++
				cap = "hmac-secret"
			}
			pin := ""
			if inf.PINSet {
				pin = " pin-set"
			}
			check(true, fmt.Sprintf("  %s %s %s%s", inf.Path, inf.Product, cap, pin))
		}
		if hasFIDOSlot && len(infos) == 0 {
			check(false, "active fido2-hmac slot but no USB HID authenticator; plug the key in and check hidraw access (lsusb). Do not install pcscd")
		}
		if hmac == 0 && len(infos) > 0 {
			check(true, "fido2: authenticators found but none advertise hmac-secret")
		}
	}

	pivToks, pivErr := (piv.HardwareFactory{}).List()
	switch {
	case pivErr != nil && len(infos) > 0:
		check(true, "piv: not available (FIDO-only Security Keys have no PIV applet; use syncsh key fido add over USB HID, not pcscd)")
	case pivErr != nil:
		check(true, "piv: "+pivErr.Error())
	case len(pivToks) == 0 && len(infos) > 0:
		check(true, "piv: no YubiKey PIV tokens; Security Keys are FIDO2-only - enroll with syncsh key fido add")
	default:
		check(true, fmt.Sprintf("piv tokens: %d", len(pivToks)))
	}
}

func reportKeyringAndDaemon(check func(bool, string), deviceID string) {
	smks, err := keyring.Get(deviceID)
	switch {
	case err != nil:
		check(false, "keyring: "+err.Error())
	case len(smks) == 0:
		if err := keyring.Available(); err != nil {
			check(false, "keyring: "+err.Error())
		} else {
			check(true, "keyring: empty (run syncsh unlock once with recovery or FIDO)")
		}
	default:
		check(true, fmt.Sprintf("keyring: %d generation key(s) stored", len(smks)))
	}
	st, err := daemon.Query()
	if err != nil {
		check(true, "daemon: "+err.Error())
		return
	}
	check(true, fmt.Sprintf("daemon installed=%v running=%v %s", st.Installed, st.Running, st.Detail))
	last, err := daemon.ReadStatus()
	if err != nil {
		check(false, "daemon status: "+err.Error())
		return
	}
	if last.At != 0 {
		msg := fmt.Sprintf("daemon last sync ok=%v", last.OK)
		if last.Error != "" {
			msg += " err=" + last.Error
		}
		if last.GCDeleted > 0 || last.GCEligible {
			msg += fmt.Sprintf(" gc_deleted=%d eligible=%v", last.GCDeleted, last.GCEligible)
		}
		check(last.OK, msg)
	}
}
