package gc

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/mistweaverco/syncsh/internal/device"
	"github.com/mistweaverco/syncsh/internal/sync/ack"
	"github.com/mistweaverco/syncsh/internal/sync/bundle"
	"github.com/mistweaverco/syncsh/internal/sync/checkpoint"
	"github.com/mistweaverco/syncsh/internal/sync/merge"
	"github.com/mistweaverco/syncsh/internal/transport"
)

type Plan struct {
	Bundles     []string
	Checkpoints []string
	Generations []string
	TmpOrphans  []string
	BlockedBy   []string
	Checkpoint  string
	Eligible    bool
}

func (p Plan) DeletedCount() int {
	return len(p.Bundles) + len(p.Checkpoints) + len(p.TmpOrphans) + len(p.Generations)
}

func Evaluate(ctx context.Context, tr transport.Transport, required []string, checkpointID string) (Plan, error) {
	acks, err := loadAcks(ctx, tr)
	if err != nil {
		return Plan{}, err
	}
	var blocked []string
	for _, id := range required {
		a, ok := acks[id]
		if !ok {
			blocked = append(blocked, id)
			continue
		}
		if checkpointID != "" && a.CheckpointID != checkpointID && !ackCovers(a, checkpointID) {
			blocked = append(blocked, id)
			continue
		}
	}
	plan := Plan{BlockedBy: blocked, Eligible: len(blocked) == 0 && len(required) > 0}
	if !plan.Eligible {
		return plan, nil
	}

	objs, err := tr.List(ctx, "")
	if err != nil {
		return plan, err
	}
	ckpt, err := resolveCheckpoint(ctx, tr, acks, required, checkpointID)
	if err != nil {
		return plan, err
	}
	plan.Checkpoint = ckpt.ID

	ckptObjs := map[string][]string{}
	ckptMeta := map[string]checkpoint.Manifest{}
	for _, o := range objs {
		base := path.Base(o.Key)
		if strings.Contains(base, ".tmp-") || strings.HasSuffix(base, ".tmp") {
			plan.TmpOrphans = append(plan.TmpOrphans, o.Key)
			continue
		}
		if id := checkpointIDFromKey(o.Key); id != "" {
			ckptObjs[id] = append(ckptObjs[id], o.Key)
			continue
		}
		if strings.HasPrefix(o.Key, "events/") && ckpt.ID != "" {
			raw, err := read(ctx, tr, o.Key)
			if err != nil {
				continue
			}
			h, err := bundle.PeekHeader(raw)
			if err != nil {
				continue
			}
			if merge.Covers(ckpt.Frontier, h.DeviceID, h.SeqEnd) {
				plan.Bundles = append(plan.Bundles, o.Key)
			}
		}
	}
	if ckpt.ID != "" {
		for _, o := range objs {
			if id := checkpointIDFromKey(o.Key); id != "" {
				if _, ok := ckptMeta[id]; ok {
					continue
				}
				if !strings.HasSuffix(o.Key, "/manifest") {
					continue
				}
				b, err := read(ctx, tr, o.Key)
				if err != nil {
					continue
				}
				m, err := checkpoint.DecodeManifest(b)
				if err == nil {
					ckptMeta[id] = m
				}
			}
		}
		for id, keys := range ckptObjs {
			if id == ckpt.ID {
				continue
			}
			if m, ok := ckptMeta[id]; ok && m.CreatedAt > ckpt.CreatedAt {
				continue
			}
			plan.Checkpoints = append(plan.Checkpoints, keys...)
		}
	}
	return plan, nil
}

func Execute(ctx context.Context, tr transport.Transport, plan Plan, dryRun bool) error {
	if dryRun || !plan.Eligible {
		return nil
	}
	for _, key := range append(append(append(plan.TmpOrphans, plan.Bundles...), plan.Checkpoints...), plan.Generations...) {
		if err := tr.Remove(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func RequiredDevices(devs []device.Device) []string {
	var ids []string
	for _, d := range devs {
		if d.Status != device.StatusRetired {
			ids = append(ids, d.ID)
		}
	}
	return ids
}

func CheckpointDue(ctx context.Context, tr transport.Transport, interval time.Duration) (bool, error) {
	m, ok, err := NewestCheckpoint(ctx, tr)
	if err != nil {
		return false, err
	}
	if !ok {
		return true, nil
	}
	if interval <= 0 {
		interval = time.Hour
	}
	return time.Since(time.UnixMilli(m.CreatedAt)) >= interval, nil
}

func NewestCheckpoint(ctx context.Context, tr transport.Transport) (checkpoint.Manifest, bool, error) {
	all, err := listCheckpoints(ctx, tr)
	if err != nil {
		return checkpoint.Manifest{}, false, err
	}
	if len(all) == 0 {
		return checkpoint.Manifest{}, false, nil
	}
	return all[0], true, nil
}

func resolveCheckpoint(ctx context.Context, tr transport.Transport, acks map[string]ack.File, required []string, checkpointID string) (checkpoint.Manifest, error) {
	if checkpointID != "" {
		b, err := read(ctx, tr, path.Join("checkpoints", checkpointID, "manifest"))
		if err != nil {
			return checkpoint.Manifest{}, nil
		}
		m, err := checkpoint.DecodeManifest(b)
		if err != nil {
			return checkpoint.Manifest{}, nil
		}
		return m, nil
	}
	all, err := listCheckpoints(ctx, tr)
	if err != nil {
		return checkpoint.Manifest{}, err
	}
	for _, m := range all {
		if allDominate(acks, required, m.Frontier) {
			return m, nil
		}
	}
	return checkpoint.Manifest{}, nil
}

func listCheckpoints(ctx context.Context, tr transport.Transport) ([]checkpoint.Manifest, error) {
	objs, err := tr.List(ctx, "checkpoints/")
	if err != nil {
		return nil, err
	}
	var out []checkpoint.Manifest
	for _, o := range objs {
		if !strings.HasSuffix(o.Key, "/manifest") {
			continue
		}
		b, err := read(ctx, tr, o.Key)
		if err != nil {
			continue
		}
		m, err := checkpoint.DecodeManifest(b)
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func allDominate(acks map[string]ack.File, required []string, frontier merge.Frontier) bool {
	for _, id := range required {
		a, ok := acks[id]
		if !ok || !merge.Dominates(a.Frontier, frontier) {
			return false
		}
	}
	return len(required) > 0
}

func checkpointIDFromKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) < 3 || parts[0] != "checkpoints" {
		return ""
	}
	return parts[1]
}

func loadAcks(ctx context.Context, tr transport.Transport) (map[string]ack.File, error) {
	objs, err := tr.List(ctx, "acks/")
	if err != nil {
		return nil, err
	}
	out := map[string]ack.File{}
	for _, o := range objs {
		b, err := read(ctx, tr, o.Key)
		if err != nil {
			return nil, err
		}
		f, err := ack.Decode(b)
		if err != nil {
			return nil, fmt.Errorf("ack %s: %w", o.Key, err)
		}
		out[f.DeviceID] = f
	}
	return out, nil
}

func ackCovers(a ack.File, checkpointID string) bool {
	return a.CheckpointID == checkpointID
}

func read(ctx context.Context, tr transport.Transport, key string) ([]byte, error) {
	r, err := tr.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf, nil
}

func EncodePlan(p Plan) ([]byte, error) { return json.MarshalIndent(p, "", "  ") }
