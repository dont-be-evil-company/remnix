package envelope

import (
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	SMKSize   = 32
	NonceSize = chacha20poly1305.NonceSizeX
)

func GenerateSMK() ([]byte, error) {
	key := make([]byte, SMKSize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

func Seal(key, nonce, plaintext, ad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != aead.NonceSize() {
		return nil, fmt.Errorf("nonce length %d", len(nonce))
	}
	return aead.Seal(nil, nonce, plaintext, ad), nil
}

func Open(key, nonce, ciphertext, ad []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	pt, err := aead.Open(nil, nonce, ciphertext, ad)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return pt, nil
}

func RandomNonce() ([]byte, error) {
	n := make([]byte, NonceSize)
	if _, err := rand.Read(n); err != nil {
		return nil, err
	}
	return n, nil
}
