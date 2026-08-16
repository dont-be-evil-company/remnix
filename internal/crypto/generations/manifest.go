package generations

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/mistweaverco/syncsh/internal/crypto/slots"
	"golang.org/x/crypto/hkdf"
)

const CurrentVersion = 1

type Manifest struct {
	Version      int          `json:"version"`
	GenerationID string       `json:"generation_id"`
	Seq          int          `json:"seq"`
	Counter      int64        `json:"counter"`
	Active       bool         `json:"active"`
	Slots        []slots.Slot `json:"slots"`
	MAC          string       `json:"mac"`
}

func macKey(smk []byte) []byte {
	r := hkdf.New(sha256.New, smk, []byte("syncsh-manifest"), []byte("mac-v1"))
	key := make([]byte, 32)
	_, _ = io.ReadFull(r, key)
	return key
}

func Sign(m *Manifest, smk []byte) error {
	m.MAC = ""
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, macKey(smk))
	mac.Write(body)
	m.MAC = hex.EncodeToString(mac.Sum(nil))
	return nil
}

func Verify(m Manifest, smk []byte) error {
	want := m.MAC
	m.MAC = ""
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, macKey(smk))
	mac.Write(body)
	got := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(got), []byte(want)) {
		return fmt.Errorf("manifest mac mismatch")
	}
	if m.Version != CurrentVersion {
		return fmt.Errorf("unsupported generation version %d", m.Version)
	}
	return nil
}
