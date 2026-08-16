package rotation

import (
	"bytes"
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/crypto/fido2"
	"github.com/dont-be-evil-company/remnix/internal/crypto/keys"
	"github.com/dont-be-evil-company/remnix/internal/crypto/piv"
	"github.com/dont-be-evil-company/remnix/internal/crypto/recovery"
	"github.com/dont-be-evil-company/remnix/internal/crypto/slots"
)

func TestBootstrapAndCredentialRotation(t *testing.T) {
	m, smk, enc, err := BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := recovery.Decode(enc)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := keys.UnwrapAny(m.Slots, keys.Unlock{RecoverySecret: secret})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, smk) {
		t.Fatal("recovery unwrap")
	}

	tok, err := piv.NewFakeToken("yk1")
	if err != nil {
		t.Fatal(err)
	}
	m, err = AddYubiKey(m, smk, tok)
	if err != nil {
		t.Fatal(err)
	}
	got, sl, err := keys.UnwrapAny(m.Slots, keys.Unlock{Tokens: []piv.Token{tok}})
	if err != nil || sl.Type != slots.TypeYubiKey {
		t.Fatalf("yubikey unwrap: %v %+v", err, sl)
	}
	if !bytes.Equal(got, smk) {
		t.Fatal("yk smk")
	}

	var ykID string
	for _, s := range m.Slots {
		if s.Type == slots.TypeYubiKey {
			ykID = s.ID
		}
	}
	m, err = RemoveSlot(m, smk, ykID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := keys.UnwrapAny(m.Slots, keys.Unlock{Tokens: []piv.Token{tok}}); err == nil {
		t.Fatal("removed yubikey should not unwrap")
	}
	if _, _, err := keys.UnwrapAny(m.Slots, keys.Unlock{RecoverySecret: secret}); err != nil {
		t.Fatal(err)
	}

	m2, _, enc2, err := RotateRecovery(m, smk)
	if err != nil {
		t.Fatal(err)
	}
	secret2, _ := recovery.Decode(enc2)
	if _, _, err := keys.UnwrapAny(m2.Slots, keys.Unlock{RecoverySecret: secret}); err == nil {
		t.Fatal("old recovery should be revoked")
	}
	if _, _, err := keys.UnwrapAny(m2.Slots, keys.Unlock{RecoverySecret: secret2}); err != nil {
		t.Fatal(err)
	}
}

func TestMasterKeyRotationKeepsOldReadable(t *testing.T) {
	m1, smk1, enc, err := BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := recovery.Decode(enc)
	m2, smk2, _, err := NewGenerationFromSlots(m1, secret, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m2.GenerationID == m1.GenerationID || bytes.Equal(smk1, smk2) {
		t.Fatal("expected new generation")
	}
	if _, _, err := keys.UnwrapAny(m1.Slots, keys.Unlock{RecoverySecret: secret}); err != nil {
		t.Fatal("old generation must remain readable")
	}
}

func TestFIDO2AddRemoveDoesNotChangeSMK(t *testing.T) {
	m, smk, enc, err := BootstrapGeneration(1, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := recovery.Decode(enc)
	if err != nil {
		t.Fatal(err)
	}
	dev := fido2.NewFakeDevice("security-key")
	m, err = AddFIDO2(m, smk, dev)
	if err != nil {
		t.Fatal(err)
	}
	got, sl, err := keys.UnwrapAny(m.Slots, keys.Unlock{FIDO2: []fido2.Device{dev}})
	if err != nil || sl.Type != slots.TypeFIDO2Hmac {
		t.Fatalf("fido2 unwrap: %v %+v", err, sl)
	}
	if !bytes.Equal(got, smk) {
		t.Fatal("fido2 smk changed")
	}
	var id string
	for _, s := range m.Slots {
		if s.Type == slots.TypeFIDO2Hmac && s.Status == slots.StatusActive {
			id = s.ID
		}
	}
	m, err = RemoveSlot(m, smk, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := keys.UnwrapAny(m.Slots, keys.Unlock{FIDO2: []fido2.Device{dev}}); err == nil {
		t.Fatal("removed fido2 slot should not unwrap")
	}
	got, _, err = keys.UnwrapAny(m.Slots, keys.Unlock{RecoverySecret: secret})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, smk) {
		t.Fatal("recovery still unwraps original smk")
	}
}

func TestNewGenerationRewrapsExistingFIDO2(t *testing.T) {
	dev := fido2.NewFakeDevice("sk")
	m1, smk1, enc, err := BootstrapGeneration(1, nil, nil, []fido2.Device{dev})
	if err != nil {
		t.Fatal(err)
	}
	secret, _ := recovery.Decode(enc)
	if _, sl, err := keys.UnwrapAny(m1.Slots, keys.Unlock{FIDO2: []fido2.Device{dev}}); err != nil || sl.Type != slots.TypeFIDO2Hmac {
		t.Fatalf("gen1 fido unwrap: %v %+v", err, sl)
	}
	m2, smk2, _, err := NewGenerationFromSlots(m1, secret, nil, []fido2.Device{dev})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(smk1, smk2) {
		t.Fatal("expected new smk")
	}
	got, sl, err := keys.UnwrapAny(m2.Slots, keys.Unlock{FIDO2: []fido2.Device{dev}})
	if err != nil || sl.Type != slots.TypeFIDO2Hmac {
		t.Fatalf("gen2 fido unwrap: %v %+v", err, sl)
	}
	if !bytes.Equal(got, smk2) {
		t.Fatal("rewrapped fido slot should unwrap new smk")
	}
	if _, _, err := keys.UnwrapAny(m1.Slots, keys.Unlock{FIDO2: []fido2.Device{dev}}); err != nil {
		t.Fatal("old generation fido slot must remain readable")
	}
}
