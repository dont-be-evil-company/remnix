package syncer

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/generations"
	"github.com/mistweaverco/syncsh/internal/crypto/keys"
	"github.com/mistweaverco/syncsh/internal/crypto/piv"
	"github.com/mistweaverco/syncsh/internal/crypto/slots"
	"github.com/mistweaverco/syncsh/internal/db"
	"github.com/mistweaverco/syncsh/internal/device"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/sync/ack"
	"github.com/mistweaverco/syncsh/internal/sync/bundle"
	"github.com/mistweaverco/syncsh/internal/sync/event"
	"github.com/mistweaverco/syncsh/internal/sync/merge"
	"github.com/mistweaverco/syncsh/internal/transport"
)

type Options struct {
	DeviceID       string
	DeviceName     string
	Hostname       string
	RecoverySecret []byte
	Tokens         []piv.Token
	FIDO2          []fido2.Device
	PINPrompt      fido2.PINPrompt
	CachedSMKs     map[string][]byte
	StoreSMKs      func(map[string][]byte)
	Transport      transport.Transport
}

type Engine struct {
	db      *db.DB
	opts    Options
	events  *event.Store
	heads   *merge.HeadStore
	keys    *keys.Store
	devices *device.Store
	history *history.Store
}

func New(d *db.DB, opts Options) *Engine {
	return &Engine{
		db:      d,
		opts:    opts,
		events:  event.NewStore(d),
		heads:   merge.NewHeadStore(d),
		keys:    keys.NewStore(d),
		devices: device.NewStore(d),
		history: history.NewStore(d),
	}
}

func (e *Engine) Sync(ctx context.Context) error {
	if s, ok := e.opts.Transport.(interface {
		Begin(context.Context) error
		End(context.Context) error
	}); ok {
		if err := s.Begin(ctx); err != nil {
			return err
		}
		defer func() { _ = s.End(ctx) }()
	}
	if err := e.pullMetadata(ctx); err != nil {
		return err
	}
	smks, active, err := e.unlockAll()
	if err != nil {
		return err
	}
	if err := e.pullCheckpoint(ctx, smks); err != nil {
		return err
	}
	if err := e.pullBundles(ctx, smks); err != nil {
		return err
	}
	if err := e.publishLocal(ctx, smks[active.GenerationID], active); err != nil {
		return err
	}
	if err := e.publishAck(ctx, ""); err != nil {
		return err
	}
	return e.publishDeviceHead(ctx)
}

func (e *Engine) Unlock() (map[string][]byte, generations.Manifest, error) {
	return e.unlockAll()
}

func (e *Engine) unlockAll() (map[string][]byte, generations.Manifest, error) {
	gens, err := e.keys.Generations()
	if err != nil {
		return nil, generations.Manifest{}, err
	}
	out := map[string][]byte{}
	var active generations.Manifest
	found := false
	for _, g := range gens {
		if smk, ok := e.opts.CachedSMKs[g.GenerationID]; ok && e.cachedSMKValid(g, smk) {
			out[g.GenerationID] = smk
			if g.Active {
				active = g
				found = true
			}
		}
	}
	if found {
		if e.opts.StoreSMKs != nil {
			e.opts.StoreSMKs(out)
		}
		return out, active, nil
	}

	fido, closer, hidErr := e.fidoDevices(gens)
	defer closer()
	u := keys.Unlock{
		RecoverySecret: e.opts.RecoverySecret,
		Tokens:         e.opts.Tokens,
		FIDO2:          fido,
	}
	for _, g := range gens {
		if _, ok := out[g.GenerationID]; ok {
			continue
		}
		smk, _, err := keys.UnwrapAny(g.Slots, u)
		if err != nil {
			if g.Active {
				if hidErr != nil && len(slots.OfType(g.Slots, slots.TypeFIDO2Hmac)) > 0 && len(u.RecoverySecret) == 0 && len(u.Tokens) == 0 {
					return nil, generations.Manifest{}, fmt.Errorf("unlock generation %s: %w", g.GenerationID, hidErr)
				}
				return nil, generations.Manifest{}, fmt.Errorf("unlock generation %s: %w", g.GenerationID, err)
			}
			continue
		}
		out[g.GenerationID] = smk
		if g.Active {
			active = g
			found = true
		}
	}
	if !found {
		return nil, generations.Manifest{}, fmt.Errorf("no active key generation")
	}
	if e.opts.StoreSMKs != nil {
		e.opts.StoreSMKs(out)
	}
	return out, active, nil
}

func (e *Engine) cachedSMKValid(g generations.Manifest, smk []byte) bool {
	if len(smk) == 0 {
		return false
	}
	if generations.Verify(g, smk) == nil {
		return true
	}
	if !g.Active {
		return true
	}
	payload, err := e.keys.TrustedPayload("remote")
	if err != nil || len(payload) == 0 {
		return false
	}
	var rm RemoteManifest
	if json.Unmarshal(payload, &rm) != nil {
		return false
	}
	return VerifyRemote(rm, smk) == nil
}

func (e *Engine) fidoDevices(gens []generations.Manifest) ([]fido2.Device, func(), error) {
	if e.opts.FIDO2 != nil {
		return e.opts.FIDO2, func() {}, nil
	}
	need := false
	for _, g := range gens {
		if len(slots.OfType(g.Slots, slots.TypeFIDO2Hmac)) > 0 {
			need = true
			break
		}
	}
	if !need {
		return nil, func() {}, nil
	}
	prompt := e.opts.PINPrompt
	if prompt == nil {
		prompt = fido2.PromptPIN
	}
	devs, err := fido2.OpenHMACDevices(prompt)
	if err != nil {
		return nil, func() {}, err
	}
	return devs, func() { fido2.CloseAll(devs) }, nil
}

func (e *Engine) pullMetadata(ctx context.Context) error {
	tr := e.opts.Transport
	var rm RemoteManifest
	haveManifest := false
	if b, err := getBytes(ctx, tr, "metadata/manifest"); err == nil {
		if err := json.Unmarshal(b, &rm); err != nil {
			return err
		}
		trusted, err := e.keys.TrustedCounter("remote")
		if err != nil {
			return err
		}
		if rm.Counter < trusted {
			return fmt.Errorf("remote manifest rollback detected (counter %d < trusted %d)", rm.Counter, trusted)
		}
		if err := e.keys.SetTrusted("remote", rm.ActiveGeneration, rm.Counter, "", b); err != nil {
			return err
		}
		haveManifest = true
		e.applyRemoteRoster(rm)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) && !os.IsNotExist(err) {
		return err
	}
	if err := e.importWantedDevices(ctx); err != nil {
		return err
	}
	if haveManifest {
		e.applyRetired(rm.Retired)
		e.applyPruned(rm.Pruned)
	}
	raw, err := readAll(ctx, tr, "keys/generations")
	if err != nil {
		return err
	}
	bySeq := map[int][]generations.Manifest{}
	for key, body := range raw {
		if !strings.HasSuffix(key, "/manifest") {
			continue
		}
		var m generations.Manifest
		if err := json.Unmarshal(body, &m); err != nil {
			return fmt.Errorf("generation manifest %s: %w", key, err)
		}
		bySeq[m.Seq] = append(bySeq[m.Seq], m)
	}
	local, err := e.keys.Generations()
	if err != nil {
		return err
	}
	localBySeq := map[int]string{}
	for _, g := range local {
		localBySeq[g.Seq] = g.GenerationID
	}
	for seq, ms := range bySeq {
		m := chooseGeneration(ms, rm.ActiveGeneration, localBySeq[seq])
		if existing, ok := localBySeq[seq]; ok && existing != m.GenerationID {
			if m.GenerationID != rm.ActiveGeneration {
				continue
			}
			if err := e.keys.DeleteGeneration(existing); err != nil {
				return err
			}
		}
		if err := e.keys.PutGeneration(m); err != nil {
			return err
		}
	}
	return nil
}

func chooseGeneration(ms []generations.Manifest, activeID, localID string) generations.Manifest {
	for _, m := range ms {
		if m.GenerationID == activeID {
			return m
		}
	}
	for _, m := range ms {
		if m.GenerationID == localID {
			return m
		}
	}
	return ms[0]
}

func (e *Engine) pullBundles(ctx context.Context, smks map[string][]byte) error {
	objs, err := e.opts.Transport.List(ctx, "events/")
	if err != nil {
		return err
	}
	local, err := e.heads.Get()
	if err != nil {
		return err
	}
	remoteHeads := merge.Clone(local)
	for _, o := range objs {
		deviceID := bundleDevice(o.Key)
		if deviceID == "" {
			continue
		}
		raw, err := getBytes(ctx, e.opts.Transport, o.Key)
		if err != nil {
			return err
		}
		genID := peekGeneration(raw)
		smk := smks[genID]
		if smk == nil {
			for _, v := range smks {
				if _, _, err := bundle.Unpack(raw, v); err == nil {
					smk = v
					break
				}
			}
		}
		if smk == nil {
			return fmt.Errorf("no key for bundle %s", o.Key)
		}
		h, evs, err := bundle.Unpack(raw, smk)
		if err != nil {
			return fmt.Errorf("bundle %s: %w", o.Key, err)
		}
		if err := e.applyEvents(evs); err != nil {
			return err
		}
		if h.SeqEnd > remoteHeads[h.DeviceID] {
			remoteHeads[h.DeviceID] = h.SeqEnd
		}
	}
	for id := range remoteHeads {
		if err := e.heads.AdvanceContiguous(id); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) applyEvents(evs []event.Event) error {
	tx, err := e.db.SQL.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, ev := range evs {
		exists, err := eventExists(tx, ev.DeviceID, ev.Seq)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		raw, err := event.Encode(ev)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO sync_events (device_id, seq, event_type, payload, applied, created_at) VALUES (?, ?, ?, ?, 0, ?)`,
			ev.DeviceID, ev.Seq, ev.Type, raw, ev.TimeUnix); err != nil {
			return err
		}
		if err := merge.Apply(tx, ev); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE sync_events SET applied = 1 WHERE device_id = ? AND seq = ?`, ev.DeviceID, ev.Seq); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (e *Engine) publishLocal(ctx context.Context, smk []byte, active generations.Manifest) error {
	var lastPub int64
	var lastPubStr string
	if err := e.db.SQL.QueryRow(`SELECT value FROM transport_state WHERE key = ?`, "published_seq:"+e.opts.DeviceID).Scan(&lastPubStr); err == nil {
		lastPub, _ = strconv.ParseInt(lastPubStr, 10, 64)
	}
	var maxSeq int64
	var maxSeqN sql.NullInt64
	_ = e.db.SQL.QueryRow(`SELECT MAX(seq) FROM sync_events WHERE device_id = ?`, e.opts.DeviceID).Scan(&maxSeqN)
	if maxSeqN.Valid {
		maxSeq = maxSeqN.Int64
	}
	if maxSeq <= lastPub {
		return nil
	}
	evs, err := e.events.Range(e.opts.DeviceID, lastPub+1, maxSeq)
	if err != nil {
		return err
	}
	if len(evs) == 0 {
		return nil
	}
	raw, _, err := bundle.Pack(e.opts.DeviceID, active.GenerationID, evs, smk)
	if err != nil {
		return err
	}
	key := path.Join("events", e.opts.DeviceID, bundle.Filename(raw))
	if err := e.opts.Transport.PutAtomic(ctx, key, bytes.NewReader(raw)); err != nil {
		return err
	}
	_, err = e.db.SQL.Exec(`
INSERT INTO transport_state (key, value) VALUES (?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, "published_seq:"+e.opts.DeviceID, fmt.Sprintf("%d", maxSeq))
	if err != nil {
		return err
	}
	return e.heads.Set(e.opts.DeviceID, maxSeq)
}

func (e *Engine) PublishAck(ctx context.Context, checkpointID string) error {
	return e.publishAck(ctx, checkpointID)
}

func (e *Engine) publishAck(ctx context.Context, checkpointID string) error {
	if d, ok, err := e.devices.Get(e.opts.DeviceID); err == nil && ok && d.Status == device.StatusRetired {
		return nil
	}
	f, err := e.heads.Get()
	if err != nil {
		return err
	}
	file := ack.New(e.opts.DeviceID, f, checkpointID)
	b, err := ack.Encode(file)
	if err != nil {
		return err
	}
	return e.opts.Transport.PutAtomic(ctx, path.Join("acks", e.opts.DeviceID+".ack"), bytes.NewReader(b))
}

func (e *Engine) publishDeviceHead(ctx context.Context) error {
	return e.publishDeviceRecord(ctx, e.opts.DeviceID)
}

func (e *Engine) publishDeviceRecord(ctx context.Context, id string) error {
	f, _ := e.heads.Get()
	df := DeviceFile{
		Version: CurrentVersion,
		ID:      id,
		Status:  device.StatusActive,
		Head:    f[id],
	}
	if d, ok, err := e.devices.Get(id); err == nil && ok {
		df.Name = d.Name
		df.Hostname = d.Hostname
		if d.Status != "" {
			df.Status = d.Status
		}
	}
	if id == e.opts.DeviceID {
		if df.Name == "" {
			df.Name = e.opts.DeviceName
		}
		if df.Hostname == "" {
			df.Hostname = e.opts.Hostname
		}
	}
	if df.Name == "" {
		df.Name = id
	}
	b, err := json.Marshal(df)
	if err != nil {
		return err
	}
	return e.opts.Transport.PutAtomic(ctx, path.Join("metadata", "devices", id+".json"), bytes.NewReader(b))
}

func eventExists(tx *sql.Tx, deviceID string, seq int64) (bool, error) {
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM sync_events WHERE device_id = ? AND seq = ?`, deviceID, seq).Scan(&n)
	return n > 0, err
}

func getBytes(ctx context.Context, tr transport.Transport, key string) ([]byte, error) {
	r, err := tr.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func readAll(ctx context.Context, tr transport.Transport, prefix string) (map[string][]byte, error) {
	objs, err := tr.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, o := range objs {
		b, err := getBytes(ctx, tr, o.Key)
		if err != nil {
			return nil, err
		}
		out[o.Key] = b
	}
	return out, nil
}

func bundleDevice(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) < 3 || parts[0] != "events" {
		return ""
	}
	return parts[1]
}

func peekGeneration(raw []byte) string {
	h, err := bundle.PeekHeader(raw)
	if err != nil {
		return ""
	}
	return h.GenerationID
}

func ptrTime(t time.Time) *time.Time { return &t }
