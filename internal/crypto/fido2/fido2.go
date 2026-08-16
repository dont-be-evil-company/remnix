package fido2

import (
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
	"golang.org/x/crypto/hkdf"
)

const (
	RPID       = "syncsh"
	ParamsV1   = 1
	hkdfSalt   = "syncsh-fido2-hmac"
	hkdfInfo   = "wrap-v1"
	aeadAD     = "syncsh-fido2-hmac-slot"
	SaltSize   = 32
	SecretSize = 32
)

// Device is a FIDO2 authenticator that can enroll a non-discoverable
// hmac-secret credential and later re-derive the 32-byte secret.
type Device interface {
	Info() Info
	Enroll() (Enrollment, error)
	Derive(credID, salt []byte) ([]byte, error)
	Close() error
}

type Info struct {
	Path       string
	Product    string
	AAGUID     string
	HMACSecret bool
	PINSet     bool
}

type Enrollment struct {
	CredentialID []byte
	Salt         []byte
	Secret       []byte
	AAGUID       string
	Product      string
	RPID         string
}

type WrapParams struct {
	Version      int    `json:"version"`
	RPID         string `json:"rp_id"`
	CredentialID []byte `json:"credential_id"`
	Salt         []byte `json:"salt"`
	AAGUID       string `json:"aaguid"`
	Product      string `json:"product"`
}

func wrapKey(hmacSecret []byte) ([]byte, error) {
	r := hkdf.New(sha256.New, hmacSecret, []byte(hkdfSalt), []byte(hkdfInfo))
	key := make([]byte, envelope.SMKSize)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}

func WrapSMK(hmacSecret, smk []byte) ([]byte, error) {
	key, err := wrapKey(hmacSecret)
	if err != nil {
		return nil, err
	}
	nonce, err := envelope.RandomNonce()
	if err != nil {
		return nil, err
	}
	ct, err := envelope.Seal(key, nonce, smk, []byte(aeadAD))
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

func UnwrapSMK(hmacSecret, wrapped []byte) ([]byte, error) {
	if len(wrapped) < envelope.NonceSize+1 {
		return nil, fmt.Errorf("wrapped smk too short")
	}
	key, err := wrapKey(hmacSecret)
	if err != nil {
		return nil, err
	}
	nonce := wrapped[:envelope.NonceSize]
	ct := wrapped[envelope.NonceSize:]
	return envelope.Open(key, nonce, ct, []byte(aeadAD))
}

func Zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
