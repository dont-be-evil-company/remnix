package syncer

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"

	"github.com/google/uuid"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/sync/checkpoint"
	"github.com/mistweaverco/syncsh/internal/sync/gc"
	"github.com/mistweaverco/syncsh/internal/sync/merge"
)

var ErrCheckpointNotCaughtUp = errors.New("local frontier does not dominate newest checkpoint")

func (e *Engine) CreateCheckpoint(ctx context.Context, smk []byte, generationID string) (checkpoint.Manifest, error) {
	behind, err := e.behindNewestCheckpoint(ctx)
	if err != nil {
		return checkpoint.Manifest{}, err
	}
	if behind {
		return checkpoint.Manifest{}, ErrCheckpointNotCaughtUp
	}
	return e.createCheckpoint(ctx, smk, generationID)
}

func (e *Engine) behindNewestCheckpoint(ctx context.Context) (bool, error) {
	if e.opts.Transport == nil {
		return false, nil
	}
	m, ok, err := gc.NewestCheckpoint(ctx, e.opts.Transport)
	if err != nil || !ok {
		return false, err
	}
	local, err := e.heads.Get()
	if err != nil {
		return false, err
	}
	return !merge.Dominates(local, m.Frontier), nil
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
	tombIDs, tombOrigins, err := e.tombstonedIndex()
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if tombIDs[ent.ID] {
			ent.Deleted = true
		}
		if ent.OriginDeviceID != "" && ent.OriginSeq != nil {
			if tombOrigins[ent.OriginDeviceID+"\x00"+fmt.Sprintf("%d", *ent.OriginSeq)] {
				ent.Deleted = true
			}
		}
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
	return e.keys.SetTrustedCheckpoint(e.trustKind(), m.ID)
}

func (e *Engine) tombstonedIndex() (ids map[string]bool, origins map[string]bool, err error) {
	rows, err := e.db.SQL.Query(`SELECT id, origin_device_id, origin_seq FROM history WHERE deleted = 1`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	ids = map[string]bool{}
	origins = map[string]bool{}
	for rows.Next() {
		var id string
		var origin sql.NullString
		var seq sql.NullInt64
		if err := rows.Scan(&id, &origin, &seq); err != nil {
			return nil, nil, err
		}
		ids[id] = true
		if origin.Valid && origin.String != "" && seq.Valid {
			origins[origin.String+"\x00"+fmt.Sprintf("%d", seq.Int64)] = true
		}
	}
	return ids, origins, rows.Err()
}

func (e *Engine) pullCheckpoint(ctx context.Context, smks map[string][]byte) error {
	m, ok, err := gc.NewestCheckpoint(ctx, e.opts.Transport)
	if err != nil || !ok {
		return err
	}
	if skip, err := e.skipStaleCheckpoint(ctx, m); err != nil || skip {
		return err
	}
	local, err := e.heads.Get()
	if err != nil {
		return err
	}
	if merge.Dominates(local, m.Frontier) {
		return e.keys.SetTrustedCheckpoint(e.trustKind(), m.ID)
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

func (e *Engine) skipStaleCheckpoint(ctx context.Context, m checkpoint.Manifest) (bool, error) {
	trustedID, err := e.keys.TrustedCheckpointID(e.trustKind())
	if err != nil || trustedID == "" || trustedID == m.ID {
		return false, err
	}
	b, err := getBytes(ctx, e.opts.Transport, path.Join("checkpoints", trustedID, "manifest"))
	if err != nil {
		return false, nil
	}
	tm, err := checkpoint.DecodeManifest(b)
	if err != nil {
		return false, nil
	}
	if merge.Dominates(tm.Frontier, m.Frontier) && !merge.Dominates(m.Frontier, tm.Frontier) {
		return true, nil
	}
	return false, nil
}
