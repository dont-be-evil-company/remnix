package fido2

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
)

func TestFakeEnrollDeriveDeterministic(t *testing.T) {
	dev := NewFakeDeviceSeeded("test-key", []byte("seed-seed-seed-seed-seed-seed-se"))
	enr, err := dev.Enroll()
	if err != nil {
		t.Fatal(err)
	}
	if enr.RPID != RPID || len(enr.Secret) != SecretSize || len(enr.Salt) != SaltSize {
		t.Fatalf("enrollment: %+v", enr)
	}
	got, err := dev.Derive(enr.CredentialID, enr.Salt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, enr.Secret) {
		t.Fatal("derive mismatch")
	}
	if _, err := dev.Derive([]byte("missing"), enr.Salt); err == nil {
		t.Fatal("expected unknown credential error")
	}
}

func TestWrapUnwrapSMK(t *testing.T) {
	smk, err := envelope.GenerateSMK()
	if err != nil {
		t.Fatal(err)
	}
	dev := NewFakeDevice("wrap")
	enr, err := dev.Enroll()
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := WrapSMK(enr.Secret, smk)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := dev.Derive(enr.CredentialID, enr.Salt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnwrapSMK(secret, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, smk) {
		t.Fatal("unwrap mismatch")
	}
	if _, err := UnwrapSMK(make([]byte, SecretSize), wrapped); err == nil {
		t.Fatal("wrong secret should fail")
	}
}

func TestAnnotateHIDAccess(t *testing.T) {
	err := Annotate(os.ErrPermission)
	if err == nil || !strings.Contains(err.Error(), "hidraw") || !strings.Contains(err.Error(), "pcscd") {
		t.Fatalf("got %v", err)
	}
	if Annotate(nil) != nil {
		t.Fatal("nil")
	}
}
