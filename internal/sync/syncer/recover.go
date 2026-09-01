package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/mistweaverco/syncsh/internal/crypto/generations"
	"github.com/mistweaverco/syncsh/internal/device"
	"github.com/mistweaverco/syncsh/internal/sync/gc"
	"github.com/mistweaverco/syncsh/internal/transport"
)

func (e *Engine) RecoverGeneration(ctx context.Context, generationID string) (generations.Manifest, error) {
	if err := transport.Dedupe(ctx, e.opts.Transport); err != nil {
		return generations.Manifest{}, err
	}
	if err := e.importWantedDevices(ctx); err != nil {
		return generations.Manifest{}, err
	}
	if b, err := getBytes(ctx, e.opts.Transport, "metadata/manifest"); err == nil {
		var rm RemoteManifest
		if json.Unmarshal(b, &rm) == nil {
			e.applyRetired(rm.Retired)
			e.applyPruned(rm.Pruned)
		}
	}
	if generationID == "" {
		ckpt, ok, err := gc.NewestCheckpoint(ctx, e.opts.Transport)
		if err != nil {
			return generations.Manifest{}, err
		}
		if !ok {
			return generations.Manifest{}, fmt.Errorf("specify a generation id; no checkpoint found on the remote")
		}
		generationID = ckpt.GenerationID
	}
	raw, err := getBytes(ctx, e.opts.Transport, path.Join("keys", "generations", generationID, "manifest"))
	if err != nil {
		return generations.Manifest{}, fmt.Errorf("read generation %s: %w", generationID, err)
	}
	var gen generations.Manifest
	if err := json.Unmarshal(raw, &gen); err != nil {
		return generations.Manifest{}, fmt.Errorf("generation manifest %s: %w", generationID, err)
	}
	gen.Active = true
	local, err := e.keys.Generations()
	if err != nil {
		return generations.Manifest{}, err
	}
	for _, g := range local {
		if g.GenerationID == gen.GenerationID {
			continue
		}
		if g.Seq == gen.Seq {
			if err := e.keys.DeleteGeneration(g.GenerationID); err != nil {
				return generations.Manifest{}, err
			}
			continue
		}
		if g.Active {
			g.Active = false
			if err := e.keys.PutGeneration(g); err != nil {
				return generations.Manifest{}, err
			}
		}
	}
	if err := e.keys.PutGeneration(gen); err != nil {
		return generations.Manifest{}, err
	}
	smks, active, err := e.unlockAll()
	if err != nil {
		return generations.Manifest{}, fmt.Errorf("unlock recovered generation %s: %w", gen.GenerationID, err)
	}
	if err := e.PublishGeneration(ctx, active, smks[active.GenerationID]); err != nil {
		return generations.Manifest{}, err
	}
	if err := e.pullCheckpoint(ctx, smks); err != nil {
		return generations.Manifest{}, err
	}
	if err := e.publishDeviceHead(ctx); err != nil {
		return generations.Manifest{}, err
	}
	return active, nil
}

func (e *Engine) importWantedDevices(ctx context.Context) error {
	wanted := map[string]bool{e.opts.DeviceID: true}
	if objs, err := e.opts.Transport.List(ctx, "acks/"); err == nil {
		for _, o := range objs {
			base := path.Base(o.Key)
			if id, ok := strings.CutSuffix(base, ".ack"); ok && id != "" {
				wanted[id] = true
			}
		}
	}
	if ckpt, ok, err := gc.NewestCheckpoint(ctx, e.opts.Transport); err == nil && ok {
		for id := range ckpt.Frontier {
			wanted[id] = true
		}
	}
	raw, err := readAll(ctx, e.opts.Transport, "metadata/devices")
	if err != nil {
		return err
	}
	for key, body := range raw {
		if !strings.HasSuffix(key, ".json") {
			continue
		}
		var df DeviceFile
		if err := json.Unmarshal(body, &df); err != nil || df.ID == "" || !wanted[df.ID] {
			continue
		}
		status := df.Status
		if status == "" {
			status = device.StatusActive
		}
		name := df.Name
		if name == "" {
			name = df.ID
		}
		_ = e.devices.Upsert(device.Device{
			ID:       df.ID,
			Name:     name,
			Hostname: df.Hostname,
			Status:   status,
		})
	}
	return nil
}

func (e *Engine) applyRemoteRoster(rm RemoteManifest) {
	skip := make(map[string]bool, len(rm.Retired)+len(rm.Pruned))
	for _, id := range rm.Retired {
		skip[id] = true
	}
	for _, id := range rm.Pruned {
		skip[id] = true
	}
	for _, id := range rm.Devices {
		if id == "" || skip[id] {
			continue
		}
		_, found, _ := e.devices.Get(id)
		if !found {
			_ = e.devices.Upsert(device.Device{ID: id, Name: id, Status: device.StatusActive})
		}
	}
}

func (e *Engine) applyRetired(ids []string) {
	at := time.Now().UTC()
	for _, id := range ids {
		if id == "" {
			continue
		}
		name, hostname := id, ""
		var retiredAt *time.Time
		if d, ok, err := e.devices.Get(id); err == nil && ok {
			name, hostname = d.Name, d.Hostname
			retiredAt = d.RetiredAt
		}
		if retiredAt == nil {
			retiredAt = ptrTime(at)
		}
		_ = e.devices.Upsert(device.Device{
			ID:        id,
			Name:      name,
			Hostname:  hostname,
			Status:    device.StatusRetired,
			RetiredAt: retiredAt,
		})
	}
}

func (e *Engine) applyPruned(ids []string) {
	for _, id := range ids {
		if id == "" || id == e.opts.DeviceID {
			continue
		}
		_ = e.devices.Delete(id)
	}
}
