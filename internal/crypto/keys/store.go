package keys

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/generations"
	"github.com/mistweaverco/syncsh/internal/crypto/piv"
	"github.com/mistweaverco/syncsh/internal/crypto/recovery"
	"github.com/mistweaverco/syncsh/internal/crypto/slots"
	"github.com/mistweaverco/syncsh/internal/db"
)

// Unlock holds credentials that can unwrap an SMK from any one active slot.
type Unlock struct {
	RecoverySecret []byte
	Tokens         []piv.Token
	FIDO2          []fido2.Device
}

type Store struct {
	db *sql.DB
}

func NewStore(d *db.DB) *Store {
	return &Store{db: d.SQL}
}

func (s *Store) PutGeneration(m generations.Manifest) error {
	var existingID string
	err := s.db.QueryRow(`SELECT id FROM key_generations WHERE seq = ?`, m.Seq).Scan(&existingID)
	if err == nil && existingID != m.GenerationID {
		return fmt.Errorf("store key generation: seq %d already held by %s", m.Seq, existingID)
	}
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("store key generation: %w", err)
	}
	_, err = s.db.Exec(`
INSERT INTO key_generations (id, seq, status, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET status = excluded.status`,
		m.GenerationID, m.Seq, statusOf(m), time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("store key generation: %w", err)
	}
	for i := range m.Slots {
		m.Slots[i].GenerationID = m.GenerationID
		if err := s.PutSlot(m.Slots[i]); err != nil {
			return err
		}
	}
	return nil
}

func statusOf(m generations.Manifest) string {
	if m.Active {
		return "active"
	}
	return "retained"
}

func (s *Store) PutSlot(sl slots.Slot) error {
	if sl.CreatedAt == 0 {
		sl.CreatedAt = time.Now().UnixMilli()
	}
	_, err := s.db.Exec(`
INSERT INTO key_slots (id, generation_id, slot_type, wrap_params, wrapped_smk, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    status = excluded.status,
    wrap_params = excluded.wrap_params,
    wrapped_smk = excluded.wrapped_smk`,
		sl.ID, sl.GenerationID, sl.Type, sl.WrapParams, sl.WrappedSMK, sl.Status, sl.CreatedAt,
	)
	return err
}

func (s *Store) Generations() ([]generations.Manifest, error) {
	rows, err := s.db.Query(`SELECT id, seq, status FROM key_generations ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []generations.Manifest
	for rows.Next() {
		var m generations.Manifest
		var status string
		if err := rows.Scan(&m.GenerationID, &m.Seq, &status); err != nil {
			return nil, err
		}
		m.Version = generations.CurrentVersion
		m.Active = status == "active"
		sl, err := s.Slots(m.GenerationID)
		if err != nil {
			return nil, err
		}
		m.Slots = sl
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Active() (generations.Manifest, bool, error) {
	all, err := s.Generations()
	if err != nil {
		return generations.Manifest{}, false, err
	}
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Active {
			return all[i], true, nil
		}
	}
	return generations.Manifest{}, false, nil
}

func (s *Store) Slots(generationID string) ([]slots.Slot, error) {
	rows, err := s.db.Query(`
SELECT id, generation_id, slot_type, wrap_params, wrapped_smk, status, created_at
FROM key_slots WHERE generation_id = ?`, generationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []slots.Slot
	for rows.Next() {
		var sl slots.Slot
		if err := rows.Scan(&sl.ID, &sl.GenerationID, &sl.Type, &sl.WrapParams, &sl.WrappedSMK, &sl.Status, &sl.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sl)
	}
	return out, rows.Err()
}

func (s *Store) DeleteGeneration(id string) error {
	if _, err := s.db.Exec(`DELETE FROM key_slots WHERE generation_id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM key_generations WHERE id = ?`, id)
	return err
}

func (s *Store) ClearUnpublished() error {
	trusted, err := s.TrustedCounter("remote")
	if err != nil {
		return err
	}
	if trusted > 0 {
		return nil
	}
	if _, err := s.db.Exec(`DELETE FROM key_slots`); err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM key_generations`)
	return err
}

func (s *Store) TrustedCounter(kind string) (int64, error) {
	var c sql.NullInt64
	err := s.db.QueryRow(`SELECT counter FROM trusted_manifests WHERE kind = ?`, kind).Scan(&c)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return c.Int64, err
}

func (s *Store) TrustedPayload(kind string) ([]byte, error) {
	var payload []byte
	err := s.db.QueryRow(`SELECT payload FROM trusted_manifests WHERE kind = ?`, kind).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return payload, err
}

func (s *Store) SetTrusted(kind, generationID string, counter int64, checkpointID string, payload []byte) error {
	_, err := s.db.Exec(`
INSERT INTO trusted_manifests (kind, generation_id, counter, checkpoint_id, payload, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(kind) DO UPDATE SET
    generation_id = excluded.generation_id,
    counter = excluded.counter,
    checkpoint_id = CASE WHEN excluded.checkpoint_id = '' THEN trusted_manifests.checkpoint_id ELSE excluded.checkpoint_id END,
    payload = excluded.payload,
    updated_at = excluded.updated_at`,
		kind, generationID, counter, checkpointID, payload, time.Now().UnixMilli())
	return err
}

func (s *Store) TrustedCheckpointID(kind string) (string, error) {
	var id sql.NullString
	err := s.db.QueryRow(`SELECT checkpoint_id FROM trusted_manifests WHERE kind = ?`, kind).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return id.String, nil
}

func (s *Store) SetTrustedCheckpoint(kind, checkpointID string) error {
	if checkpointID == "" {
		return nil
	}
	_, err := s.db.Exec(`UPDATE trusted_manifests SET checkpoint_id = ?, updated_at = ? WHERE kind = ?`,
		checkpointID, time.Now().UnixMilli(), kind)
	return err
}

func NewRecoverySlot(generationID string, smk, secret []byte) (slots.Slot, error) {
	wrapped, err := recovery.WrapSMK(secret, smk)
	if err != nil {
		return slots.Slot{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return slots.Slot{}, err
	}
	return slots.Slot{
		ID:           id.String(),
		Type:         slots.TypeRecovery,
		GenerationID: generationID,
		WrappedSMK:   wrapped,
		Status:       slots.StatusActive,
		CreatedAt:    time.Now().UnixMilli(),
	}, nil
}

func NewYubiKeySlot(generationID string, smk []byte, tok piv.Token) (slots.Slot, error) {
	pub, err := tok.Public()
	if err != nil {
		return slots.Slot{}, err
	}
	params, wrapped, err := piv.WrapSMK(pub, smk)
	if err != nil {
		return slots.Slot{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return slots.Slot{}, err
	}
	meta, _ := json.Marshal(map[string]string{"serial": tok.Serial(), "label": tok.Label()})
	params = append(params, 0)
	params = append(params, meta...)
	return slots.Slot{
		ID:           id.String(),
		Type:         slots.TypeYubiKey,
		GenerationID: generationID,
		WrapParams:   params,
		WrappedSMK:   wrapped,
		Status:       slots.StatusActive,
		CreatedAt:    time.Now().UnixMilli(),
		Label:        tok.Serial(),
	}, nil
}

func NewFIDO2Slot(generationID string, smk []byte, dev fido2.Device) (slots.Slot, error) {
	enr, err := dev.Enroll()
	if err != nil {
		return slots.Slot{}, err
	}
	defer fido2.Zero(enr.Secret)
	return newFIDO2SlotFromEnrollment(generationID, smk, enr)
}

func NewFIDO2SlotFromParams(generationID string, smk, hmacSecret []byte, p fido2.WrapParams) (slots.Slot, error) {
	return newFIDO2SlotFromEnrollment(generationID, smk, fido2.Enrollment{
		CredentialID: p.CredentialID,
		Salt:         p.Salt,
		Secret:       hmacSecret,
		AAGUID:       p.AAGUID,
		Product:      p.Product,
		RPID:         p.RPID,
	})
}

func newFIDO2SlotFromEnrollment(generationID string, smk []byte, enr fido2.Enrollment) (slots.Slot, error) {
	wrapped, err := fido2.WrapSMK(enr.Secret, smk)
	if err != nil {
		return slots.Slot{}, err
	}
	if enr.RPID == "" {
		enr.RPID = fido2.RPID
	}
	params, err := json.Marshal(fido2.WrapParams{
		Version:      fido2.ParamsV1,
		RPID:         enr.RPID,
		CredentialID: enr.CredentialID,
		Salt:         enr.Salt,
		AAGUID:       enr.AAGUID,
		Product:      enr.Product,
	})
	if err != nil {
		return slots.Slot{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return slots.Slot{}, err
	}
	return slots.Slot{
		ID:           id.String(),
		Type:         slots.TypeFIDO2Hmac,
		GenerationID: generationID,
		WrapParams:   params,
		WrappedSMK:   wrapped,
		Status:       slots.StatusActive,
		CreatedAt:    time.Now().UnixMilli(),
		Label:        enr.Product,
	}, nil
}

func Unwrap(sl slots.Slot, u Unlock) ([]byte, error) {
	switch sl.Type {
	case slots.TypeRecovery:
		if len(u.RecoverySecret) == 0 {
			return nil, fmt.Errorf("recovery key required")
		}
		return recovery.UnwrapSMK(u.RecoverySecret, sl.WrappedSMK)
	case slots.TypeYubiKey:
		var last error
		for _, tok := range u.Tokens {
			smk, err := tok.Decrypt(sl.WrappedSMK)
			if err == nil {
				return smk, nil
			}
			last = err
		}
		if last == nil {
			return nil, fmt.Errorf("no yubikey available for slot %s", sl.ID)
		}
		return nil, last
	case slots.TypeFIDO2Hmac:
		return unwrapFIDO2(sl, u.FIDO2)
	default:
		return nil, fmt.Errorf("unknown slot type %s", sl.Type)
	}
}

func unwrapFIDO2(sl slots.Slot, devices []fido2.Device) ([]byte, error) {
	var p fido2.WrapParams
	if err := json.Unmarshal(sl.WrapParams, &p); err != nil {
		return nil, fmt.Errorf("fido2 wrap params: %w", err)
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no fido2 authenticator available for slot %s", sl.ID)
	}
	var last error
	for _, dev := range devices {
		secret, err := dev.Derive(p.CredentialID, p.Salt)
		if err != nil {
			last = err
			continue
		}
		smk, err := fido2.UnwrapSMK(secret, sl.WrappedSMK)
		fido2.Zero(secret)
		if err == nil {
			return smk, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("no fido2 authenticator available for slot %s", sl.ID)
	}
	return nil, last
}

func UnwrapAny(sls []slots.Slot, u Unlock) ([]byte, slots.Slot, error) {
	var last error
	for _, sl := range slots.Active(sls) {
		smk, err := Unwrap(sl, u)
		if err == nil {
			return smk, sl, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("no active key slots")
	}
	return nil, slots.Slot{}, last
}
