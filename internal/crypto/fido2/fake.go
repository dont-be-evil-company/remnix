package fido2

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
)

// FakeDevice is a deterministic hmac-secret stand-in for tests.
type FakeDevice struct {
	product string
	aaguid  string
	seed    []byte
	mu      sync.Mutex
	creds   map[string]struct{}
}

func NewFakeDevice(product string) *FakeDevice {
	seed := make([]byte, 32)
	_, _ = rand.Read(seed)
	return &FakeDevice{
		product: product,
		aaguid:  "00000000-0000-0000-0000-000000000001",
		seed:    seed,
		creds:   map[string]struct{}{},
	}
}

func NewFakeDeviceSeeded(product string, seed []byte) *FakeDevice {
	s := make([]byte, len(seed))
	copy(s, seed)
	return &FakeDevice{
		product: product,
		aaguid:  "00000000-0000-0000-0000-000000000001",
		seed:    s,
		creds:   map[string]struct{}{},
	}
}

func (f *FakeDevice) Info() Info {
	return Info{
		Path:       "fake:" + f.product,
		Product:    f.product,
		AAGUID:     f.aaguid,
		HMACSecret: true,
		PINSet:     false,
	}
}

func (f *FakeDevice) Enroll() (Enrollment, error) {
	credID := make([]byte, 32)
	salt := make([]byte, SaltSize)
	if _, err := rand.Read(credID); err != nil {
		return Enrollment{}, err
	}
	if _, err := rand.Read(salt); err != nil {
		return Enrollment{}, err
	}
	f.mu.Lock()
	f.creds[hex.EncodeToString(credID)] = struct{}{}
	f.mu.Unlock()
	secret := fakeDerive(f.seed, credID, salt)
	return Enrollment{
		CredentialID: credID,
		Salt:         salt,
		Secret:       secret,
		AAGUID:       f.aaguid,
		Product:      f.product,
		RPID:         RPID,
	}, nil
}

func (f *FakeDevice) Derive(credID, salt []byte) ([]byte, error) {
	f.mu.Lock()
	_, ok := f.creds[hex.EncodeToString(credID)]
	f.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown fido2 credential")
	}
	return fakeDerive(f.seed, credID, salt), nil
}

func (f *FakeDevice) Close() error { return nil }

func fakeDerive(seed, credID, salt []byte) []byte {
	mac := hmac.New(sha256.New, seed)
	mac.Write(credID)
	mac.Write(salt)
	out := mac.Sum(nil)
	return out[:SecretSize]
}
