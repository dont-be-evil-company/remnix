package piv

import (
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"strings"

	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
	"golang.org/x/crypto/hkdf"
)

type Token interface {
	Serial() string
	Label() string
	Public() (crypto.PublicKey, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}

type Factory interface {
	List() ([]Token, error)
	Generate(slotHint string) (Token, error)
}

func Annotate(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "resource manager is not running"),
		strings.Contains(s, "resource manager has shut down"),
		strings.Contains(s, "connecting to pcsc"):
		return fmt.Errorf("%w\n\nThe YubiKey is invisible until pcscd is running, even if the token is plugged in.\nStart it and retry:\n  sudo systemctl start pcscd.socket\n  sudo systemctl start pcscd\nThen check with: pcsc_scan", err)
	case strings.Contains(s, "not available in this build"):
		return fmt.Errorf("%w\nRebuild with PIV support: go build -tags piv ./cmd/syncsh", err)
	default:
		return err
	}
}

func WrapSMK(pub crypto.PublicKey, smk []byte) (params, wrapped []byte, err error) {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		ct, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, k, smk, []byte("syncsh-piv"))
		if err != nil {
			return nil, nil, err
		}
		return []byte(`{"alg":"rsa-oaep-sha256"}`), ct, nil
	case *ecdsa.PublicKey:
		return wrapECDH(k, smk)
	default:
		return nil, nil, fmt.Errorf("unsupported public key type %T", pub)
	}
}

func wrapECDH(pub *ecdsa.PublicKey, smk []byte) ([]byte, []byte, error) {
	curve, err := ecdhCurve(pub.Curve)
	if err != nil {
		return nil, nil, err
	}
	eph, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	peer, err := ecdsaToECDH(pub)
	if err != nil {
		return nil, nil, err
	}
	secret, err := eph.ECDH(peer)
	if err != nil {
		return nil, nil, err
	}
	key, err := deriveWrapKey(secret)
	if err != nil {
		return nil, nil, err
	}
	nonce, err := envelope.RandomNonce()
	if err != nil {
		return nil, nil, err
	}
	ct, err := envelope.Seal(key, nonce, smk, []byte("syncsh-piv-ecdh"))
	if err != nil {
		return nil, nil, err
	}
	payload := append(append([]byte{}, nonce...), ct...)
	params := []byte(fmt.Sprintf(`{"alg":"ecdh-xchacha20","eph":"%x"}`, eph.PublicKey().Bytes()))
	return params, payload, nil
}

func UnwrapSMK(tok Token, params, wrapped []byte) ([]byte, error) {
	return tok.Decrypt(wrapped)
}

func deriveWrapKey(secret []byte) ([]byte, error) {
	r := hkdf.New(sha256.New, secret, []byte("syncsh-piv"), []byte("wrap-v1"))
	key := make([]byte, envelope.SMKSize)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}

func ecdhCurve(c elliptic.Curve) (ecdh.Curve, error) {
	switch c {
	case elliptic.P256():
		return ecdh.P256(), nil
	case elliptic.P384():
		return ecdh.P384(), nil
	default:
		return nil, fmt.Errorf("unsupported curve")
	}
}

func ecdsaToECDH(pub *ecdsa.PublicKey) (*ecdh.PublicKey, error) {
	curve, err := ecdhCurve(pub.Curve)
	if err != nil {
		return nil, err
	}
	b := elliptic.Marshal(pub.Curve, pub.X, pub.Y)
	return curve.NewPublicKey(b)
}

type FakeToken struct {
	serial string
	label  string
	key    *rsa.PrivateKey
}

func NewFakeToken(serial string) (*FakeToken, error) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &FakeToken{serial: serial, label: "fake-" + serial, key: k}, nil
}

func (f *FakeToken) Serial() string { return f.serial }
func (f *FakeToken) Label() string  { return f.label }
func (f *FakeToken) Public() (crypto.PublicKey, error) {
	return &f.key.PublicKey, nil
}
func (f *FakeToken) Decrypt(ciphertext []byte) ([]byte, error) {
	return rsa.DecryptOAEP(sha256.New(), rand.Reader, f.key, ciphertext, []byte("syncsh-piv"))
}

type FakeFactory struct {
	Tokens []*FakeToken
}

func (f *FakeFactory) List() ([]Token, error) {
	out := make([]Token, len(f.Tokens))
	for i, t := range f.Tokens {
		out[i] = t
	}
	return out, nil
}

func (f *FakeFactory) Generate(slotHint string) (Token, error) {
	serial := slotHint
	if serial == "" {
		var n uint64
		_ = binary.Read(rand.Reader, binary.LittleEndian, &n)
		serial = fmt.Sprintf("fake-%d", n)
	}
	tok, err := NewFakeToken(serial)
	if err != nil {
		return nil, err
	}
	f.Tokens = append(f.Tokens, tok)
	return tok, nil
}
