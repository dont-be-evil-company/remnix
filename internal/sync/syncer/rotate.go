package syncer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/dont-be-evil-company/remnix/internal/crypto/generations"
	"github.com/dont-be-evil-company/remnix/internal/crypto/keys"
	"github.com/dont-be-evil-company/remnix/internal/crypto/rotation"
)

func rotationNotCommitted(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("key rotation was not committed: %w\nthe current generation remains active; rerun `remnix key rotate`", err)
}

func rotationCommittedLocal(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("key rotation committed remotely, but local key state could not be updated: %w\nrerun `remnix key rotate` or `remnix key status` to reconcile", err)
}

// RotateGeneration stages seq+1, publishes it, then activates it via
// metadata/manifest. Retry resumes the same generation instead of minting another.
func (e *Engine) RotateGeneration(ctx context.Context) (string, error) {
	var encoded string
	err := e.withRemote(ctx, func() error {
		var err error
		encoded, err = e.rotateGeneration(ctx)
		return err
	})
	return encoded, err
}

func (e *Engine) rotateGeneration(ctx context.Context) (string, error) {
	if e.opts.Transport == nil {
		return "", rotationNotCommitted(fmt.Errorf("no remote transport"))
	}
	smks, active, err := e.Unlock()
	if err != nil {
		return "", rotationNotCommitted(err)
	}
	e.rememberSMKs(smks)

	rot, hasRot, err := e.keys.Rotation()
	if err != nil {
		return "", err
	}
	remoteGens, err := e.listRemoteGenerations(ctx)
	if err != nil {
		return "", rotationNotCommitted(err)
	}
	rm, haveRM, err := e.loadRemoteManifest(ctx)
	if err != nil {
		return "", rotationNotCommitted(err)
	}

	if hasRot {
		old, next, oldSMK, newSMK, err := e.loadJournalPair(rot, smks, remoteGens)
		if err != nil {
			return "", err
		}
		if err := e.rejectConflictingSeq(generationsAtSeq(remoteGens, rot.NewSeq), rot.NewGenerationID); err != nil {
			return "", err
		}
		return e.finishRotation(ctx, old, next, oldSMK, newSMK, rot.RecoveryBech32)
	}

	if haveRM {
		if next, newSMK, encoded, ok, err := e.remoteAheadOfLocal(ctx, active, smks, rm); err != nil {
			return "", err
		} else if ok {
			old := active
			oldSMK := smks[old.GenerationID]
			if err := e.keys.StageRotation(next, old, encoded); err != nil {
				if _, exists, _ := e.keys.Generation(next.GenerationID); !exists {
					return "", rotationNotCommitted(err)
				}
			}
			return e.finishRotation(ctx, old, next, oldSMK, newSMK, encoded)
		}
	}

	remoteAtSeq := generationsAtSeq(remoteGens, active.Seq+1)
	if err := e.rejectConflictingSeq(remoteAtSeq, ""); err != nil {
		return "", err
	}
	if len(remoteAtSeq) == 1 {
		cand := remoteAtSeq[0]
		smk, err := e.smkFor(cand, smks)
		if err != nil {
			return "", fmt.Errorf("a generation with seq %d already exists (%s) and cannot be proven to belong to this rotation; run remnix doctor or remnix key recover", cand.Seq, cand.GenerationID)
		}
		cand.Active = false
		if err := e.keys.StageRotation(cand, active, ""); err != nil {
			return "", rotationNotCommitted(err)
		}
		return e.finishRotation(ctx, active, cand, smks[active.GenerationID], smk, "")
	}

	next, newSMK, encoded, err := rotation.NewGenerationFromSlots(active, e.opts.RecoverySecret, e.opts.Tokens, e.opts.FIDO2)
	if err != nil {
		return "", rotationNotCommitted(err)
	}
	if err := e.verifyNewGeneration(next, newSMK); err != nil {
		return "", rotationNotCommitted(err)
	}
	if err := e.keys.StageRotation(next, active, encoded); err != nil {
		return "", rotationNotCommitted(err)
	}
	return e.finishRotation(ctx, active, next, smks[active.GenerationID], newSMK, encoded)
}

func (e *Engine) finishRotation(ctx context.Context, old, next generations.Manifest, oldSMK, newSMK []byte, encoded string) (string, error) {
	e.rememberSMKs(map[string][]byte{old.GenerationID: oldSMK, next.GenerationID: newSMK})
	pub := next
	pub.Active = true
	if err := generations.Sign(&pub, newSMK); err != nil {
		return encoded, rotationNotCommitted(err)
	}
	if err := e.publishGenerationMaterial(ctx, pub); err != nil {
		return encoded, rotationNotCommitted(fmt.Errorf("failed to publish new generation: %w", err))
	}
	_ = e.keys.SetRotationPhase(keys.RotationGenerationPublished)

	committed, err := e.activateRotatedGeneration(ctx, next, oldSMK, newSMK)
	if err != nil && !committed {
		return encoded, err
	}
	if err := e.reconcileRotatedLocal(old, next, newSMK); err != nil {
		return encoded, rotationCommittedLocal(err)
	}
	_ = e.keys.SetRotationPhase(keys.RotationComplete)
	return encoded, nil
}

func (e *Engine) loadJournalPair(rot keys.Rotation, smks map[string][]byte, remoteGens []generations.Manifest) (old, next generations.Manifest, oldSMK, newSMK []byte, err error) {
	var ok bool
	old, ok, err = e.keys.Generation(rot.OldGenerationID)
	if err != nil {
		return generations.Manifest{}, generations.Manifest{}, nil, nil, err
	}
	if !ok {
		return generations.Manifest{}, generations.Manifest{}, nil, nil, rotationNotCommitted(fmt.Errorf("in-progress rotation is missing old generation %s", rot.OldGenerationID))
	}
	next, ok, err = e.keys.Generation(rot.NewGenerationID)
	if err != nil {
		return generations.Manifest{}, generations.Manifest{}, nil, nil, err
	}
	if !ok {
		if remote := findGeneration(remoteGens, rot.NewGenerationID); remote != nil {
			next = *remote
			next.Active = false
			if err := e.keys.PutGeneration(next); err != nil {
				return generations.Manifest{}, generations.Manifest{}, nil, nil, rotationNotCommitted(err)
			}
		} else {
			return generations.Manifest{}, generations.Manifest{}, nil, nil, rotationNotCommitted(fmt.Errorf("in-progress rotation generation %s is missing", rot.NewGenerationID))
		}
	}
	oldSMK, err = e.smkFor(old, smks)
	if err != nil {
		return generations.Manifest{}, generations.Manifest{}, nil, nil, rotationNotCommitted(err)
	}
	newSMK, err = e.smkFor(next, smks)
	if err != nil {
		return generations.Manifest{}, generations.Manifest{}, nil, nil, rotationNotCommitted(err)
	}
	return old, next, oldSMK, newSMK, nil
}

func (e *Engine) remoteAheadOfLocal(ctx context.Context, local generations.Manifest, smks map[string][]byte, rm RemoteManifest) (generations.Manifest, []byte, string, bool, error) {
	if rm.ActiveGeneration == "" || rm.ActiveGeneration == local.GenerationID {
		return generations.Manifest{}, nil, "", false, nil
	}
	next, ok, err := e.keys.Generation(rm.ActiveGeneration)
	if err != nil {
		return generations.Manifest{}, nil, "", false, err
	}
	if !ok {
		raw, err := getBytes(ctx, e.opts.Transport, path.Join("keys", "generations", rm.ActiveGeneration, "manifest"))
		if err != nil {
			return generations.Manifest{}, nil, "", false, fmt.Errorf("remote already selects generation %s; run remnix doctor or remnix key recover", rm.ActiveGeneration)
		}
		if err := json.Unmarshal(raw, &next); err != nil {
			return generations.Manifest{}, nil, "", false, err
		}
	}
	smk, err := e.smkFor(next, smks)
	if err != nil {
		return generations.Manifest{}, nil, "", false, fmt.Errorf("remote already selects generation %s, which this device cannot unlock; run remnix key recover", rm.ActiveGeneration)
	}
	if VerifyRemote(rm, smk) != nil {
		return generations.Manifest{}, nil, "", false, nil
	}
	next.Active = false
	return next, smk, "", true, nil
}

func (e *Engine) activateRotatedGeneration(ctx context.Context, next generations.Manifest, oldSMK, newSMK []byte) (committed bool, err error) {
	putErr := e.publishRemoteRoster(ctx, next.GenerationID, next.Counter, newSMK, oldSMK, nil)
	rm, have, loadErr := e.loadRemoteManifest(ctx)
	if loadErr == nil && have && VerifyRemote(rm, newSMK) == nil && rm.ActiveGeneration == next.GenerationID {
		_ = e.keys.SetRotationPhase(keys.RotationManifestCommitted)
		return true, nil
	}
	if putErr != nil {
		return false, rotationNotCommitted(fmt.Errorf("failed to activate new generation: %w", putErr))
	}
	_ = e.keys.SetRotationPhase(keys.RotationManifestCommitted)
	return true, nil
}

func (e *Engine) reconcileRotatedLocal(old, next generations.Manifest, newSMK []byte) error {
	if err := e.keys.PromoteRotation(old.GenerationID, next.GenerationID); err != nil {
		old.Active = false
		next.Active = true
		if err := e.keys.PutGeneration(old); err != nil {
			return err
		}
		if err := e.keys.PutGeneration(next); err != nil {
			return err
		}
	}
	e.rememberSMKs(map[string][]byte{next.GenerationID: newSMK})
	return nil
}

func (e *Engine) rememberSMKs(smks map[string][]byte) {
	if e.opts.CachedSMKs == nil {
		e.opts.CachedSMKs = map[string][]byte{}
	}
	for id, smk := range smks {
		if len(smk) == 0 {
			continue
		}
		e.opts.CachedSMKs[id] = smk
	}
	if e.opts.StoreSMKs != nil {
		e.opts.StoreSMKs(e.opts.CachedSMKs)
	}
}

func (e *Engine) verifyNewGeneration(next generations.Manifest, smk []byte) error {
	if err := generations.Verify(next, smk); err != nil {
		return fmt.Errorf("verify new generation: %w", err)
	}
	got, _, err := keys.UnwrapAny(next.Slots, keys.Unlock{
		RecoverySecret: e.opts.RecoverySecret,
		Tokens:         e.opts.Tokens,
		FIDO2:          e.opts.FIDO2,
	})
	if err != nil {
		return fmt.Errorf("new generation is not recoverable: %w", err)
	}
	if !bytes.Equal(got, smk) {
		return fmt.Errorf("new generation slot unwrap mismatch")
	}
	return nil
}

func (e *Engine) smkFor(m generations.Manifest, smks map[string][]byte) ([]byte, error) {
	if smk, ok := smks[m.GenerationID]; ok && len(smk) > 0 && e.cachedSMKValid(m, smk) {
		return smk, nil
	}
	if smk, ok := e.opts.CachedSMKs[m.GenerationID]; ok && len(smk) > 0 && e.cachedSMKValid(m, smk) {
		return smk, nil
	}
	smk, _, err := keys.UnwrapAny(m.Slots, keys.Unlock{
		RecoverySecret: e.opts.RecoverySecret,
		Tokens:         e.opts.Tokens,
		FIDO2:          e.opts.FIDO2,
	})
	return smk, err
}

func (e *Engine) listRemoteGenerations(ctx context.Context) ([]generations.Manifest, error) {
	raw, err := readAll(ctx, e.opts.Transport, "keys/generations")
	if err != nil {
		return nil, err
	}
	var out []generations.Manifest
	for key, body := range raw {
		if !strings.HasSuffix(key, "/manifest") {
			continue
		}
		var m generations.Manifest
		if err := json.Unmarshal(body, &m); err != nil {
			return nil, fmt.Errorf("generation manifest %s: %w", key, err)
		}
		out = append(out, m)
	}
	return out, nil
}

func (e *Engine) loadRemoteManifest(ctx context.Context) (RemoteManifest, bool, error) {
	raw, err := getBytes(ctx, e.opts.Transport, "metadata/manifest")
	if err != nil {
		return RemoteManifest{}, false, nil
	}
	var rm RemoteManifest
	if err := json.Unmarshal(raw, &rm); err != nil {
		return RemoteManifest{}, false, err
	}
	return rm, true, nil
}

func (e *Engine) rejectConflictingSeq(atSeq []generations.Manifest, expectedID string) error {
	if expectedID == "" {
		if len(atSeq) > 1 {
			return fmt.Errorf("conflicting generations claim seq %d; run remnix doctor or remnix key recover", atSeq[0].Seq)
		}
		return nil
	}
	for _, g := range atSeq {
		if g.GenerationID == expectedID {
			continue
		}
		return fmt.Errorf("conflicting generation %s also claims seq %d; run remnix doctor or remnix key recover", g.GenerationID, g.Seq)
	}
	return nil
}

func generationsAtSeq(gens []generations.Manifest, seq int) []generations.Manifest {
	var out []generations.Manifest
	for _, g := range gens {
		if g.Seq == seq {
			out = append(out, g)
		}
	}
	return out
}

func findGeneration(gens []generations.Manifest, id string) *generations.Manifest {
	for i := range gens {
		if gens[i].GenerationID == id {
			return &gens[i]
		}
	}
	return nil
}
