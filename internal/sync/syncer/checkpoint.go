package syncer

import (
	"bytes"
	"context"
	"fmt"
	"path"

	"github.com/google/uuid"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/sync/checkpoint"
	"github.com/mistweaverco/syncsh/internal/sync/gc"
	"github.com/mistweaverco/syncsh/internal/sync/merge"
)

func (e *Engine) CreateCheckpoint(ctx context.Context, smk []byte, generationID string) (checkpoint.Manifest, error) {
	return e.createCheckpoint(ctx, smk, generationID)
}

func (e *Engine) createCheckpoint(ctx context.Context, smk []byte, generationID string) (checkpoint.Manifest, error) {
	f, err := e.heads.Get()
	if err != nil {
		return checkpoint.Manifest{}, err
	}
	entries, err := e.history.List(history.Filter{IncludeDeleted: true, Limit: 0})
	if err != nil {
		return checkpoint.Manifest{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return checkpoint.Manifest{}, err
	}
	m := checkpoint.NewManifest(id.String(), generationID, f)
	mb, err := checkpoint.EncodeManifest(m)
	if err != nil {
		return checkpoint.Manifest{}, err
	}
	nonce, ct, err := checkpoint.PackSnapshot(entries, smk)
	if err != nil {
		return checkpoint.Manifest{}, err
	}
	base := path.Join("checkpoints", m.ID)
	if err := e.opts.Transport.PutAtomic(ctx, path.Join(base, "manifest"), bytes.NewReader(mb)); err != nil {
		return checkpoint.Manifest{}, err
	}
	if err := e.opts.Transport.PutAtomic(ctx, path.Join(base, "snapshot"), bytes.NewReader(checkpoint.EncodeFile(nonce, ct))); err != nil {
		return checkpoint.Manifest{}, err
	}
	return m, nil
}

func (e *Engine) BootstrapFromCheckpoint(ctx context.Context, smk []byte, m checkpoint.Manifest, snapJSON []byte) error {
	nonce, ct, err := checkpoint.DecodeFile(snapJSON)
	if err != nil {
		return err
	}
	entries, err := checkpoint.UnpackSnapshot(smk, nonce, ct)
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if _, err := e.history.Insert(ent); err != nil {
			return err
		}
	}
	local, err := e.heads.Get()
	if err != nil {
		return err
	}
	for id, seq := range m.Frontier {
		if seq > local[id] {
			if err := e.heads.Set(id, seq); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) pullCheckpoint(ctx context.Context, smks map[string][]byte) error {
	m, ok, err := gc.NewestCheckpoint(ctx, e.opts.Transport)
	if err != nil || !ok {
		return err
	}
	local, err := e.heads.Get()
	if err != nil {
		return err
	}
	if merge.Dominates(local, m.Frontier) {
		return nil
	}
	snap, err := getBytes(ctx, e.opts.Transport, path.Join("checkpoints", m.ID, "snapshot"))
	if err != nil {
		return fmt.Errorf("checkpoint snapshot %s: %w", m.ID, err)
	}
	var last error
	if smk := smks[m.GenerationID]; len(smk) > 0 {
		last = e.BootstrapFromCheckpoint(ctx, smk, m, snap)
		if last == nil {
			return nil
		}
	}
	for id, smk := range smks {
		if id == m.GenerationID {
			continue
		}
		if err := e.BootstrapFromCheckpoint(ctx, smk, m, snap); err == nil {
			return nil
		} else {
			last = err
		}
	}
	if last != nil {
		return fmt.Errorf("checkpoint %s: %w", m.ID, last)
	}
	return fmt.Errorf("no key for checkpoint %s generation %s", m.ID, m.GenerationID)
}
