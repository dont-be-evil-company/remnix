package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"

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

func (e *Engine) PublishGeneration(ctx context.Context, m generations.Manifest, smk []byte) error {
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
	rm := RemoteManifest{
		Version:          CurrentVersion,
		Counter:          m.Counter,
		ActiveGeneration: m.GenerationID,
		Devices:          []string{e.opts.DeviceID},
	}
	if ids, err := e.devices.ActiveIDs(); err == nil && len(ids) > 0 {
		rm.Devices = ids
	}
	all, _ := e.devices.List()
	for _, d := range all {
		if d.Status == device.StatusRetired {
			rm.Retired = append(rm.Retired, d.ID)
		}
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
