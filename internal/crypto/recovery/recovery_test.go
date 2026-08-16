package recovery

import (
	"bytes"
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/crypto/envelope"
)

func TestRecoveryWrapIndependent(t *testing.T) {
	smk, err := envelope.GenerateSMK()
	if err != nil {
		t.Fatal(err)
	}
	secret, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := Encode(secret)
	if err != nil {
		t.Fatal(err)
	}
	if enc[:6] != "remnix" {
		t.Fatalf("encoding %s", enc)
	}
	got, err := Decode(enc)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := WrapSMK(got, smk)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := UnwrapSMK(got, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, smk) {
		t.Fatal("unwrap mismatch")
	}
}
