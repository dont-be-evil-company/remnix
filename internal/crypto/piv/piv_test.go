package piv

import (
	"bytes"
	"testing"

	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
)

func TestFakeTokenWrapUnwrap(t *testing.T) {
	smk, _ := envelope.GenerateSMK()
	tok, err := NewFakeToken("abc")
	if err != nil {
		t.Fatal(err)
	}
	pub, err := tok.Public()
	if err != nil {
		t.Fatal(err)
	}
	_, wrapped, err := WrapSMK(pub, smk)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tok.Decrypt(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, smk) {
		t.Fatal("mismatch")
	}
}
