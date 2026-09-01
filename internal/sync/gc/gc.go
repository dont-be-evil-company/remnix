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
	if err := transport.Dedupe(ctx, tr); err != nil {
		return Plan{}, err
	}
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

	objs, err := listLayoutObjects(ctx, tr)
	if err != nil {
		return plan, err
	}
	ckptObjs := map[string]bool{}
	ckptMeta := map[string]checkpoint.Manifest{}
	for _, o := range objs {
		base := path.Base(o.Key)
		if strings.Contains(base, ".tmp-") || strings.HasSuffix(base, ".tmp") {
			plan.TmpOrphans = append(plan.TmpOrphans, o.Key)
			continue
		}
		if id := checkpointIDFromKey(o.Key); id != "" {
			ckptObjs[id] = true
		}
	}
	if dirs, err := tr.ListDirs(ctx, "checkpoints/"); err == nil {
		for _, dir := range dirs {
			if id := checkpointIDFromKey(dir + "/"); id != "" {
				ckptObjs[id] = true
			}
		}
	}

	if plan.Eligible {
		ckpt, err := resolveCheckpoint(ctx, tr, acks, required, checkpointID)
		if err != nil {
			return plan, err
		}
		plan.Checkpoint = ckpt.ID
		for _, o := range objs {
			if !strings.HasPrefix(o.Key, "events/") || ckpt.ID == "" {
				continue
			}
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
	for _, o := range objs {
		if !strings.HasSuffix(o.Key, "/manifest") {
			continue
		}
		id := checkpointIDFromKey(o.Key)
		if id == "" {
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
	keepID := plan.Checkpoint
	var keepMeta checkpoint.Manifest
	haveKeep := false
	if best, ok := pickBestCheckpoint(ckptMeta); ok {
		keepID = best.ID
		keepMeta = best
		haveKeep = true
	} else if keepID != "" {
		keepMeta, haveKeep = ckptMeta[keepID]
	} else if m, ok, err := NewestCheckpoint(ctx, tr); err == nil && ok {
		keepID = m.ID
		keepMeta, haveKeep = m, true
	}
	canDeleteFull := plan.Eligible && plan.Checkpoint != ""
	for id := range ckptObjs {
		if id == "" || id == keepID {
			continue
		}
		m, hasMeta := ckptMeta[id]
		if hasMeta && haveKeep {
			keepDom := merge.Dominates(keepMeta.Frontier, m.Frontier)
			candDom := merge.Dominates(m.Frontier, keepMeta.Frontier)
			if candDom && !keepDom {
				continue
			}
			if keepDom {
				plan.Checkpoints = append(plan.Checkpoints, path.Join("checkpoints", id))
				continue
			}
			if m.CreatedAt > keepMeta.CreatedAt {
				continue
			}
		}
		if hasMeta && !canDeleteFull {
			continue
		}
		plan.Checkpoints = append(plan.Checkpoints, path.Join("checkpoints", id))
	}
	sort.Strings(plan.Checkpoints)
	return plan, nil
}

func Execute(ctx context.Context, tr transport.Transport, plan Plan, dryRun bool) error {
	if dryRun {
		return nil
	}
	keys := append([]string{}, plan.TmpOrphans...)
	keys = append(keys, plan.Checkpoints...)
	if plan.Eligible {
		keys = append(keys, plan.Bundles...)
		keys = append(keys, plan.Generations...)
	}
	for _, key := range keys {
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

func pickBestCheckpoint(byID map[string]checkpoint.Manifest) (checkpoint.Manifest, bool) {
	var all []checkpoint.Manifest
	for _, m := range byID {
		all = append(all, m)
	}
	if len(all) == 0 {
		return checkpoint.Manifest{}, false
	}
	sortCheckpoints(all)
	return all[0], true
}

func betterCheckpoint(a, b checkpoint.Manifest) bool {
	aDom := merge.Dominates(a.Frontier, b.Frontier)
	bDom := merge.Dominates(b.Frontier, a.Frontier)
	if aDom && !bDom {
		return true
	}
	if bDom && !aDom {
		return false
	}
	sa, sb := merge.Score(a.Frontier), merge.Score(b.Frontier)
	if sa != sb {
		return sa > sb
	}
	if a.CreatedAt != b.CreatedAt {
		return a.CreatedAt > b.CreatedAt
	}
	return a.ID > b.ID
}

func sortCheckpoints(all []checkpoint.Manifest) {
	sort.Slice(all, func(i, j int) bool { return betterCheckpoint(all[i], all[j]) })
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
	sortCheckpoints(out)
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
	parts := strings.Split(strings.Trim(key, "/"), "/")
	if len(parts) < 2 || parts[0] != "checkpoints" || parts[1] == "" {
		return ""
	}
	return parts[1]
}

func listLayoutObjects(ctx context.Context, tr transport.Transport) ([]transport.Object, error) {
	var out []transport.Object
	seen := map[string]bool{}
	add := func(objs []transport.Object) {
		for _, o := range objs {
			if seen[o.Key] {
				continue
			}
			seen[o.Key] = true
			out = append(out, o)
		}
	}
	for _, p := range []string{"metadata", "keys", "events", "checkpoints", "acks"} {
		var objs []transport.Object
		var err error
		if p == "events" {
			objs, err = listEventObjects(ctx, tr)
		} else {
			objs, err = tr.List(ctx, p)
		}
		if err != nil {
			return nil, err
		}
		add(objs)
	}
	return out, nil
}

type shallowLister interface {
	ListShallow(ctx context.Context, prefix string) ([]transport.Object, error)
}

func listEventObjects(ctx context.Context, tr transport.Transport) ([]transport.Object, error) {
	s, ok := tr.(shallowLister)
	if !ok {
		return tr.List(ctx, "events")
	}
	files, err := s.ListShallow(ctx, "events")
	if err != nil {
		return nil, err
	}
	dirs, err := tr.ListDirs(ctx, "events")
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		more, err := s.ListShallow(ctx, d)
		if err != nil {
			return nil, err
		}
		files = append(files, more...)
	}
	return files, nil
}

func loadAcks(ctx context.Context, tr transport.Transport) (map[string]ack.File, error) {
	objs, err := tr.List(ctx, "acks/")
	if err != nil {
		return nil, err
	}
	out := map[string]ack.File{}
	seen := map[string]bool{}
	for _, o := range objs {
		if seen[o.Key] {
			continue
		}
		seen[o.Key] = true
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
