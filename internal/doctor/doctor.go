package doctor

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dont-be-evil-company/remnix/internal/app"
	"github.com/dont-be-evil-company/remnix/internal/config"
	"github.com/dont-be-evil-company/remnix/internal/crypto/fido2"
	"github.com/dont-be-evil-company/remnix/internal/crypto/keyring"
	"github.com/dont-be-evil-company/remnix/internal/crypto/keys"
	"github.com/dont-be-evil-company/remnix/internal/crypto/piv"
	"github.com/dont-be-evil-company/remnix/internal/crypto/slots"
	"github.com/dont-be-evil-company/remnix/internal/daemon"
	"github.com/dont-be-evil-company/remnix/internal/db"
	"github.com/dont-be-evil-company/remnix/internal/device"
	"github.com/dont-be-evil-company/remnix/internal/redact"
	"github.com/dont-be-evil-company/remnix/internal/repository"
	"github.com/dont-be-evil-company/remnix/internal/setup"
	"github.com/dont-be-evil-company/remnix/internal/sync/gc"
	"github.com/dont-be-evil-company/remnix/internal/sync/merge"
	"github.com/dont-be-evil-company/remnix/internal/transport"
	"github.com/google/uuid"
)

type Report struct {
	OK       bool
	Findings []string
}

func Run(ctx context.Context, a *app.App, w io.Writer) error {
	r := Report{OK: true}
	check := func(ok bool, msg string) {
		msg = redact.String(msg)
		fmt.Fprintln(w, msg)
		if !ok {
			r.OK = false
			r.Findings = append(r.Findings, msg)
		}
	}

	check(a.Config.Version == config.CurrentVersion, fmt.Sprintf("config version: %d", a.Config.Version))
	if p, ok := config.LeftoverPortableRcloneConfig(); ok {
		check(false, "rclone credentials still in "+p+" (portable config dir); they belong in "+config.RcloneConfigPath())
	}
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
		thinApplied := false
		for _, s := range st {
			if !s.Applied || s.Mismatch {
				check(false, "migration issue: "+s.ID)
			}
			if s.ID == "0003_thin_events" && s.Applied {
				thinApplied = true
			}
		}
		check(true, fmt.Sprintf("migrations: %d applied", len(st)))
		if thinApplied {
			n, err := db.HistoryEventPayloads(a.DB.SQL)
			if err != nil {
				check(false, "sync_events payloads: "+err.Error())
			} else if n > 0 {
				check(false, fmt.Sprintf("sync_events still stores %d history payloads", n))
			} else {
				check(true, "sync_events: history payloads stripped")
			}
		}
	}
	if info, err := db.PageInfoOf(a.DB.SQL); err != nil {
		check(false, "db size: "+err.Error())
	} else {
		check(true, fmt.Sprintf("db size: %s (%d pages, freelist %d)", db.FormatBytes(info.Bytes), info.PageCount, info.Freelist))
	}
	if btrees, err := db.BTreeStats(a.DB.SQL); err == nil {
		limit := 8
		if len(btrees) < limit {
			limit = len(btrees)
		}
		for _, s := range btrees[:limit] {
			check(true, fmt.Sprintf("  db %s %s", s.Name, db.FormatBytes(s.Size)))
		}
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

	if st, err := setup.LoadState(); err != nil {
		check(false, "setup-state: "+err.Error())
	} else if st != nil && st.Phase != "" {
		check(false, fmt.Sprintf("partial setup: phase=%s generation=%s (retry remnix setup, or delete setup-state.json to start over)", st.Phase, st.GenerationID))
	}

	if !a.Config.Sync.IsEnabled() {
		check(true, "sync: disabled (local history only)")
	} else {
		check(true, "rclone engine: "+string(a.Config.Sync.RcloneEngineOrDefault()))
		eps := a.Config.Sync.EnabledEndpoints()
		if len(eps) == 0 {
			check(false, "endpoints: none enabled")
		}
		for _, ep := range a.Config.Sync.Endpoints {
			state := "disabled"
			if ep.Enabled {
				state = "enabled"
			}
			msg := fmt.Sprintf("endpoint %s type=%s %s", ep.ID, ep.Type, state)
			if ep.Provider != "" {
				msg += " provider=" + ep.Provider
			}
			if ep.Path != "" {
				msg += " path=" + ep.Path
			}
			check(true, msg)
		}
		for _, ep := range eps {
			tr, err := a.OpenTransport(ep)
			if err != nil {
				check(false, ep.ID+" transport: "+err.Error())
				continue
			}
			st, _ := tr.HealthCheck(ctx)
			check(st.State == transport.HealthOK || st.State == transport.HealthNotFound, ep.ID+" health: "+string(st.State)+" "+st.Message)
			rep, err := repository.Probe(ctx, tr)
			if err != nil {
				check(false, ep.ID+" repository probe: "+err.Error())
			} else {
				check(rep.Result == repository.Valid || rep.Result == repository.Empty, ep.ID+" repository: "+rep.Result.String()+" "+rep.Message)
			}
			if tr.Capabilities().ListingExpensive {
				check(true, ep.ID+" remote list: skipped (cloud folder listing is expensive)")
			} else {
				dirs, err := tr.ListDirs(ctx, "")
				if err != nil {
					check(false, ep.ID+" remote list: "+err.Error())
				} else {
					check(true, fmt.Sprintf("%s remote directories: %d", ep.ID, len(dirs)))
				}
			}
			hid := uuid.NewString()
			hkey := "metadata/health/" + a.Config.DeviceID + "/" + hid
			if err := tr.PutAtomic(ctx, hkey, strings.NewReader("ok")); err != nil {
				check(true, ep.ID+" write probe skipped: "+err.Error())
			} else {
				_ = tr.Remove(ctx, hkey)
				check(true, ep.ID+" write probe: ok")
			}
			plan, err := gc.Evaluate(ctx, tr, gc.RequiredDevices(devs), "")
			if err == nil {
				check(true, fmt.Sprintf("%s gc eligible: %v blocked_by=%v", ep.ID, plan.Eligible, plan.BlockedBy))
			}
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
		check(true, "piv: not available (FIDO-only Security Keys have no PIV applet; use remnix key fido add over USB HID, not pcscd)")
	case pivErr != nil:
		check(true, "piv: "+pivErr.Error())
	case len(pivToks) == 0 && len(infos) > 0:
		check(true, "piv: no YubiKey PIV tokens; Security Keys are FIDO2-only - enroll with remnix key fido add")
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
			check(true, "keyring: empty (run remnix unlock once with recovery or FIDO)")
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
		msg += last.FormatTiming()
		if last.GCDeleted > 0 || last.GCEligible {
			msg += fmt.Sprintf(" gc_deleted=%d eligible=%v", last.GCDeleted, last.GCEligible)
		}
		check(last.OK, msg)
	}
}
