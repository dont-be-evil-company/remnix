package envelope

import (
	"bytes"
	"testing"
)

func TestSealOpen(t *testing.T) {
	key, err := GenerateSMK()
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := RandomNonce()
	if err != nil {
		t.Fatal(err)
	}
	ad := []byte("header")
	ct, err := Seal(key, nonce, []byte("hello"), ad)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := Open(key, nonce, ct, ad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pt, []byte("hello")) {
		t.Fatalf("%q", pt)
	}
	if _, err := Open(key, nonce, ct, []byte("other")); err == nil {
		t.Fatal("expected ad mismatch to fail")
	}
}
