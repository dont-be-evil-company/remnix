package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/mistweaverco/syncsh/internal/crypto/generations"
	"github.com/mistweaverco/syncsh/internal/device"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/sync/event"
	"github.com/mistweaverco/syncsh/internal/transport"
)

var ErrRemoteInitialized = errors.New("remote already initialized; use 'syncsh device add' to join, or 'syncsh key recover' if setup created a conflicting generation")

func RemoteInitialized(ctx context.Context, tr transport.Transport) (bool, error) {
	_, err := getBytes(ctx, tr, "metadata/manifest")
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) || os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (e *Engine) InitializeRemote(ctx context.Context, m generations.Manifest, smk []byte) error {
	exists, err := RemoteInitialized(ctx, e.opts.Transport)
	if err != nil {
		return err
	}
	if exists {
		return ErrRemoteInitialized
	}
	if err := e.keys.PutGeneration(m); err != nil {
		return err
	}
	if err := e.devices.Upsert(device.Device{
		ID:       e.opts.DeviceID,
		Name:     e.opts.DeviceName,
		Hostname: e.opts.Hostname,
		Status:   device.StatusActive,
	}); err != nil {
		return err
	}
	genJSON, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := e.opts.Transport.PutAtomic(ctx, path.Join("keys", "generations", m.GenerationID, "manifest"), bytes.NewReader(genJSON)); err != nil {
		return err
	}
	rm := RemoteManifest{
		Version:          CurrentVersion,
		Counter:          1,
		ActiveGeneration: m.GenerationID,
		Devices:          []string{e.opts.DeviceID},
	}
	if err := SignRemote(&rm, smk); err != nil {
		return err
	}
	body, err := json.Marshal(rm)
	if err != nil {
		return err
	}
	if err := e.opts.Transport.PutAtomic(ctx, "metadata/manifest", bytes.NewReader(body)); err != nil {
		return err
	}
	if err := e.opts.Transport.PutAtomic(ctx, "metadata/version", bytes.NewReader([]byte(`{"version":1}`))); err != nil {
		return err
	}
	if err := e.keys.SetTrusted("remote", m.GenerationID, rm.Counter, "", body); err != nil {
		return err
	}
	return e.publishDeviceHead(ctx)
}

func (e *Engine) EnqueueHistoryCreated(entry history.Entry) error {
	seq, err := e.events.NextSeq(e.opts.DeviceID)
	if err != nil {
		return err
	}
	ev, err := event.NewHistoryCreated(e.opts.DeviceID, seq, entry)
	if err != nil {
		return err
	}
	if err := e.events.Append(ev); err != nil {
		return err
	}
	if _, err := e.db.SQL.Exec(`UPDATE history SET origin_device_id = ?, origin_seq = ? WHERE id = ?`, e.opts.DeviceID, seq, entry.ID); err != nil {
		return err
	}
	if err := e.events.MarkApplied(e.opts.DeviceID, seq); err != nil {
		return err
	}
	return e.heads.Set(e.opts.DeviceID, seq)
}

func (e *Engine) EnqueueHistoryTombstoned(entry history.Entry) error {
	seq, err := e.events.NextSeq(e.opts.DeviceID)
	if err != nil {
		return err
	}
	var originSeq int64
	if entry.OriginSeq != nil {
		originSeq = *entry.OriginSeq
	}
	ev, err := newTombstone(e.opts.DeviceID, seq, entry.ID, entry.OriginDeviceID, originSeq)
	if err != nil {
		return err
	}
	if err := e.events.Append(ev); err != nil {
		return err
	}
	if err := e.history.Tombstone(entry.ID); err != nil {
		return err
	}
	if err := e.events.MarkApplied(e.opts.DeviceID, seq); err != nil {
		return err
	}
	return e.heads.Set(e.opts.DeviceID, seq)
}

func (e *Engine) withRemote(ctx context.Context, fn func() error) error {
	if s, ok := e.opts.Transport.(interface {
		Begin(context.Context) error
		End(context.Context) error
	}); ok {
		if err := s.Begin(ctx); err != nil {
			return err
		}
		defer func() { _ = s.End(ctx) }()
	}
	return fn()
}

func (e *Engine) RetireDevice(ctx context.Context, id string, at time.Time) error {
	return e.withRemote(ctx, func() error {
		if err := e.devices.Retire(id, at); err != nil {
			return err
		}
		if err := e.publishDeviceRecord(ctx, id); err != nil {
			return err
		}
		if err := e.opts.Transport.Remove(ctx, path.Join("acks", id+".ack")); err != nil {
			return err
		}
		smks, active, err := e.Unlock()
		if err != nil {
			return fmt.Errorf("unlock to publish retirement: %w", err)
		}
		return e.publishGeneration(ctx, active, smks[active.GenerationID], nil)
	})
}

func (e *Engine) PruneDevice(ctx context.Context, id string) error {
	if id == e.opts.DeviceID {
		return fmt.Errorf("cannot prune this device (%s)", id)
	}
	return e.withRemote(ctx, func() error {
		if err := e.devices.Delete(id); err != nil {
			return err
		}
		if err := e.removeRemoteDeviceArtifacts(ctx, id); err != nil {
			return err
		}
		smks, active, err := e.Unlock()
		if err != nil {
			return fmt.Errorf("unlock to publish prune: %w", err)
		}
		return e.publishGeneration(ctx, active, smks[active.GenerationID], []string{id})
	})
}

func (e *Engine) removeRemoteDeviceArtifacts(ctx context.Context, id string) error {
	if err := e.opts.Transport.Remove(ctx, path.Join("metadata", "devices", id+".json")); err != nil {
		return err
	}
	if err := e.opts.Transport.Remove(ctx, path.Join("acks", id+".ack")); err != nil {
		return err
	}
	return e.opts.Transport.Remove(ctx, path.Join("events", id))
}

func (e *Engine) PublishGeneration(ctx context.Context, m generations.Manifest, smk []byte) error {
	return e.publishGeneration(ctx, m, smk, nil)
}

func (e *Engine) publishGeneration(ctx context.Context, m generations.Manifest, smk []byte, extraPruned []string) error {
	if err := e.keys.PutGeneration(m); err != nil {
		return err
	}
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := e.opts.Transport.PutAtomic(ctx, path.Join("keys", "generations", m.GenerationID, "manifest"), bytes.NewReader(body)); err != nil {
		return err
	}
	pruned := map[string]bool{}
	retired := map[string]bool{}
	for _, id := range extraPruned {
		pruned[id] = true
	}
	all, _ := e.devices.List()
	for _, d := range all {
		if d.Status == device.StatusRetired {
			retired[d.ID] = true
		}
	}
	if raw, err := getBytes(ctx, e.opts.Transport, "metadata/manifest"); err == nil {
		var existing RemoteManifest
		if json.Unmarshal(raw, &existing) == nil && VerifyRemote(existing, smk) == nil {
			for _, id := range existing.Pruned {
				pruned[id] = true
			}
			for _, id := range existing.Retired {
				retired[id] = true
			}
		}
	}
	delete(pruned, e.opts.DeviceID)
	for id := range pruned {
		delete(retired, id)
	}
	devices := []string{e.opts.DeviceID}
	if ids, err := e.devices.ActiveIDs(); err == nil && len(ids) > 0 {
		devices = ids
	}
	var active []string
	for _, id := range devices {
		if pruned[id] || retired[id] {
			continue
		}
		active = append(active, id)
	}
	if len(active) == 0 {
		active = []string{e.opts.DeviceID}
		delete(pruned, e.opts.DeviceID)
		delete(retired, e.opts.DeviceID)
	}
	var retiredIDs, prunedIDs []string
	for id := range retired {
		retiredIDs = append(retiredIDs, id)
	}
	for id := range pruned {
		prunedIDs = append(prunedIDs, id)
	}
	rm := RemoteManifest{
		Version:          CurrentVersion,
		Counter:          m.Counter,
		ActiveGeneration: m.GenerationID,
		Devices:          uniqueSorted(active),
		Retired:          uniqueSorted(retiredIDs),
		Pruned:           uniqueSorted(prunedIDs),
	}
	trusted, _ := e.keys.TrustedCounter("remote")
	if trusted >= rm.Counter {
		rm.Counter = trusted + 1
	}
	if err := SignRemote(&rm, smk); err != nil {
		return err
	}
	raw, err := json.Marshal(rm)
	if err != nil {
		return err
	}
	if err := e.opts.Transport.PutAtomic(ctx, "metadata/manifest", bytes.NewReader(raw)); err != nil {
		return err
	}
	return e.keys.SetTrusted("remote", m.GenerationID, rm.Counter, "", raw)
}
