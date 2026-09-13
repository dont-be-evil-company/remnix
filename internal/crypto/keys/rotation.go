package keys

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/crypto/generations"
)

const (
	RotationPrepared            = "prepared"
	RotationGenerationPublished = "generation_published"
	RotationManifestCommitted   = "manifest_committed"
	RotationLocalReconciled     = "local_reconciled"
	RotationComplete            = "complete"
)

type Rotation struct {
	Phase           string
	OldGenerationID string
	NewGenerationID string
	OldSeq          int
	NewSeq          int
	RecoveryBech32  string
	UpdatedAt       int64
}

func (s *Store) Rotation() (Rotation, bool, error) {
	var r Rotation
	err := s.db.QueryRow(`
SELECT phase, old_generation_id, new_generation_id, old_seq, new_seq, COALESCE(recovery_bech32, ''), updated_at
FROM key_rotation WHERE id = 1`).Scan(
		&r.Phase, &r.OldGenerationID, &r.NewGenerationID, &r.OldSeq, &r.NewSeq, &r.RecoveryBech32, &r.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return Rotation{}, false, nil
	}
	if err != nil {
		return Rotation{}, false, err
	}
	return r, true, nil
}

// StageRotation records a retained (not yet active) next generation and a
// durable rotation journal in one transaction.
func (s *Store) StageRotation(next generations.Manifest, old generations.Manifest, encoded string) error {
	next.Active = false
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.putGeneration(tx, next); err != nil {
		return err
	}
	_, err = tx.Exec(`
INSERT INTO key_rotation (id, phase, old_generation_id, new_generation_id, old_seq, new_seq, recovery_bech32, updated_at)
VALUES (1, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    phase = excluded.phase,
    old_generation_id = excluded.old_generation_id,
    new_generation_id = excluded.new_generation_id,
    old_seq = excluded.old_seq,
    new_seq = excluded.new_seq,
    recovery_bech32 = excluded.recovery_bech32,
    updated_at = excluded.updated_at`,
		RotationPrepared, old.GenerationID, next.GenerationID, old.Seq, next.Seq, encoded, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("store key rotation: %w", err)
	}
	return tx.Commit()
}

func (s *Store) SetRotationPhase(phase string) error {
	res, err := s.db.Exec(`UPDATE key_rotation SET phase = ?, updated_at = ? WHERE id = 1`, phase, time.Now().UnixMilli())
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("store key rotation: no in-progress rotation")
	}
	return nil
}

func (s *Store) PromoteRotation(oldID, newID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE key_generations SET status = 'retained' WHERE id = ?`, oldID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE key_generations SET status = 'active' WHERE id = ?`, newID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE key_rotation SET phase = ?, updated_at = ? WHERE id = 1`, RotationLocalReconciled, time.Now().UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ClearRotation() error {
	_, err := s.db.Exec(`DELETE FROM key_rotation`)
	return err
}
