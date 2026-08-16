package rotation

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
	"github.com/mistweaverco/syncsh/internal/crypto/fido2"
	"github.com/mistweaverco/syncsh/internal/crypto/generations"
	"github.com/mistweaverco/syncsh/internal/crypto/keys"
	"github.com/mistweaverco/syncsh/internal/crypto/piv"
	"github.com/mistweaverco/syncsh/internal/crypto/recovery"
	"github.com/mistweaverco/syncsh/internal/crypto/slots"
)

func AddYubiKey(m generations.Manifest, smk []byte, tok piv.Token) (generations.Manifest, error) {
	sl, err := keys.NewYubiKeySlot(m.GenerationID, smk, tok)
	if err != nil {
		return m, err
	}
	got, err := tok.Decrypt(sl.WrappedSMK)
	if err != nil {
		return m, fmt.Errorf("verify new yubikey slot: %w", err)
	}
	if string(got) != string(smk) {
		return m, fmt.Errorf("new yubikey slot failed verification")
	}
	m.Slots = append(m.Slots, sl)
	m.Counter++
	if err := generations.Sign(&m, smk); err != nil {
		return m, err
	}
	return m, nil
}

func AddFIDO2(m generations.Manifest, smk []byte, dev fido2.Device) (generations.Manifest, error) {
	sl, err := keys.NewFIDO2Slot(m.GenerationID, smk, dev)
	if err != nil {
		return m, err
	}
	got, err := keys.Unwrap(sl, keys.Unlock{FIDO2: []fido2.Device{dev}})
	if err != nil {
		return m, fmt.Errorf("verify new fido2 slot: %w", err)
	}
	if string(got) != string(smk) {
		return m, fmt.Errorf("new fido2 slot failed verification")
	}
	m.Slots = append(m.Slots, sl)
	m.Counter++
	if err := generations.Sign(&m, smk); err != nil {
		return m, err
	}
	return m, nil
}

func RemoveSlot(m generations.Manifest, smk []byte, slotID string) (generations.Manifest, error) {
	var remaining []slots.Slot
	found := false
	for _, sl := range m.Slots {
		if sl.ID == slotID && sl.Status == slots.StatusActive {
			sl.Status = slots.StatusRevoked
			found = true
		}
		remaining = append(remaining, sl)
	}
	if !found {
		return m, fmt.Errorf("active slot %s not found", slotID)
	}
	active := slots.Active(remaining)
	if len(active) == 0 {
		return m, fmt.Errorf("refusing to remove last active unlock slot")
	}
	m.Slots = remaining
	m.Counter++
	if err := generations.Sign(&m, smk); err != nil {
		return m, err
	}
	return m, nil
}

func RotateRecovery(m generations.Manifest, smk []byte) (generations.Manifest, []byte, string, error) {
	secret, err := recovery.Generate()
	if err != nil {
		return m, nil, "", err
	}
	sl, err := keys.NewRecoverySlot(m.GenerationID, smk, secret)
	if err != nil {
		return m, nil, "", err
	}
	if _, err := recovery.UnwrapSMK(secret, sl.WrappedSMK); err != nil {
		return m, nil, "", fmt.Errorf("verify recovery slot: %w", err)
	}
	encoded, err := recovery.Encode(secret)
	if err != nil {
		return m, nil, "", err
	}
	for i := range m.Slots {
		if m.Slots[i].Type == slots.TypeRecovery && m.Slots[i].Status == slots.StatusActive {
			m.Slots[i].Status = slots.StatusRevoked
		}
	}
	m.Slots = append(m.Slots, sl)
	m.Counter++
	if err := generations.Sign(&m, smk); err != nil {
		return m, nil, "", err
	}
	return m, secret, encoded, nil
}

func RotateMasterKey(old generations.Manifest, oldSMK []byte, wrapSlots []slots.Slot) (generations.Manifest, []byte, error) {
	smk, err := envelope.GenerateSMK()
	if err != nil {
		return generations.Manifest{}, nil, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return generations.Manifest{}, nil, err
	}
	m := generations.Manifest{
		Version:      generations.CurrentVersion,
		GenerationID: id.String(),
		Seq:          old.Seq + 1,
		Counter:      1,
		Active:       true,
	}
	for _, sl := range wrapSlots {
		switch sl.Type {
		case slots.TypeRecovery:
			return generations.Manifest{}, nil, fmt.Errorf("recovery wrap requires the new secret; use BootstrapGeneration")
		}
		_ = sl
	}
	_ = oldSMK
	if err := generations.Sign(&m, smk); err != nil {
		return generations.Manifest{}, nil, err
	}
	return m, smk, nil
}

func BootstrapGeneration(seq int, recoverySecret []byte, tokens []piv.Token, fido []fido2.Device) (generations.Manifest, []byte, string, error) {
	smk, err := envelope.GenerateSMK()
	if err != nil {
		return generations.Manifest{}, nil, "", err
	}
	if recoverySecret == nil {
		recoverySecret, err = recovery.Generate()
		if err != nil {
			return generations.Manifest{}, nil, "", err
		}
	}
	id, err := uuid.NewV7()
	if err != nil {
		return generations.Manifest{}, nil, "", err
	}
	sl, err := keys.NewRecoverySlot(id.String(), smk, recoverySecret)
	if err != nil {
		return generations.Manifest{}, nil, "", err
	}
	m := generations.Manifest{
		Version:      generations.CurrentVersion,
		GenerationID: id.String(),
		Seq:          seq,
		Counter:      1,
		Active:       true,
		Slots:        []slots.Slot{sl},
	}
	for _, tok := range tokens {
		yk, err := keys.NewYubiKeySlot(id.String(), smk, tok)
		if err != nil {
			return generations.Manifest{}, nil, "", err
		}
		m.Slots = append(m.Slots, yk)
	}
	for _, dev := range fido {
		fs, err := keys.NewFIDO2Slot(id.String(), smk, dev)
		if err != nil {
			return generations.Manifest{}, nil, "", err
		}
		m.Slots = append(m.Slots, fs)
	}
	if err := generations.Sign(&m, smk); err != nil {
		return generations.Manifest{}, nil, "", err
	}
	enc, err := recovery.Encode(recoverySecret)
	if err != nil {
		return generations.Manifest{}, nil, "", err
	}
	return m, smk, enc, nil
}

func NewGenerationFromSlots(old generations.Manifest, recoverySecret []byte, tokens []piv.Token, fido []fido2.Device) (generations.Manifest, []byte, string, error) {
	m, smk, enc, err := BootstrapGeneration(old.Seq+1, recoverySecret, tokens, nil)
	if err != nil {
		return generations.Manifest{}, nil, "", err
	}
	for _, sl := range slots.OfType(old.Slots, slots.TypeFIDO2Hmac) {
		rewrapped, err := rewrapFIDO2Slot(m.GenerationID, smk, sl, fido)
		if err != nil {
			return generations.Manifest{}, nil, "", err
		}
		m.Slots = append(m.Slots, rewrapped)
	}
	if len(slots.OfType(old.Slots, slots.TypeFIDO2Hmac)) == 0 {
		for _, dev := range fido {
			fs, err := keys.NewFIDO2Slot(m.GenerationID, smk, dev)
			if err != nil {
				return generations.Manifest{}, nil, "", err
			}
			m.Slots = append(m.Slots, fs)
		}
	}
	if err := generations.Sign(&m, smk); err != nil {
		return generations.Manifest{}, nil, "", err
	}
	return m, smk, enc, nil
}

func rewrapFIDO2Slot(generationID string, smk []byte, sl slots.Slot, devices []fido2.Device) (slots.Slot, error) {
	var p fido2.WrapParams
	if err := json.Unmarshal(sl.WrapParams, &p); err != nil {
		return slots.Slot{}, fmt.Errorf("fido2 wrap params: %w", err)
	}
	secret, err := deriveFIDO2(p, devices)
	if err != nil {
		return slots.Slot{}, err
	}
	defer fido2.Zero(secret)
	return keys.NewFIDO2SlotFromParams(generationID, smk, secret, p)
}

func deriveFIDO2(p fido2.WrapParams, devices []fido2.Device) ([]byte, error) {
	var last error
	for _, dev := range devices {
		secret, err := dev.Derive(p.CredentialID, p.Salt)
		if err != nil {
			last = err
			continue
		}
		return secret, nil
	}
	if last == nil {
		last = fmt.Errorf("no fido2 authenticator available to re-wrap slot")
	}
	return nil, last
}
