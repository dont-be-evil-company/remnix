package recovery

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"

	"github.com/btcsuite/btcd/btcutil/bech32"
	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
	"golang.org/x/crypto/hkdf"
)

const hrp = "syncsh"

func Generate() ([]byte, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return secret, nil
}

func Encode(secret []byte) (string, error) {
	conv, err := bech32.ConvertBits(secret, 8, 5, true)
	if err != nil {
		return "", err
	}
	return bech32.Encode(hrp, conv)
}

func Decode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	hrpGot, data, err := bech32.Decode(s)
	if err != nil {
		return nil, fmt.Errorf("recovery key: %w", err)
	}
	if hrpGot != hrp {
		return nil, fmt.Errorf("recovery key: unexpected prefix %q", hrpGot)
	}
	secret, err := bech32.ConvertBits(data, 5, 8, false)
	if err != nil {
		return nil, err
	}
	if len(secret) < 32 {
		return nil, fmt.Errorf("recovery key: too short")
	}
	return secret[:32], nil
}

func wrapKey(secret []byte) ([]byte, error) {
	r := hkdf.New(sha256.New, secret, []byte("syncsh-recovery"), []byte("wrap-v1"))
	key := make([]byte, envelope.SMKSize)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}

func WrapSMK(secret, smk []byte) ([]byte, error) {
	key, err := wrapKey(secret)
	if err != nil {
		return nil, err
	}
	nonce, err := envelope.RandomNonce()
	if err != nil {
		return nil, err
	}
	ct, err := envelope.Seal(key, nonce, smk, []byte("syncsh-recovery-slot"))
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(nonce)+len(ct))
	out = append(out, nonce...)
	out = append(out, ct...)
	return out, nil
}

func UnwrapSMK(secret, wrapped []byte) ([]byte, error) {
	if len(wrapped) < envelope.NonceSize+1 {
		return nil, fmt.Errorf("wrapped smk too short")
	}
	key, err := wrapKey(secret)
	if err != nil {
		return nil, err
	}
	nonce := wrapped[:envelope.NonceSize]
	ct := wrapped[envelope.NonceSize:]
	return envelope.Open(key, nonce, ct, []byte("syncsh-recovery-slot"))
}
