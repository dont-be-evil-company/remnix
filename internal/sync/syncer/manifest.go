package syncer

import (
	"encoding/json"
	"fmt"
	"time"

	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
	"github.com/mistweaverco/syncsh/internal/crypto/generations"
	"io"

	"golang.org/x/crypto/hkdf"
)

const CurrentVersion = 1

type RemoteManifest struct {
	Version          int      `json:"version"`
	Counter          int64    `json:"counter"`
	ActiveGeneration string   `json:"active_generation"`
	Devices          []string `json:"devices"`
	Retired          []string `json:"retired"`
	UpdatedAt        int64    `json:"updated_at"`
	MAC              string   `json:"mac"`
}

type DeviceFile struct {
	Version  int    `json:"version"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Status   string `json:"status"`
	Head     int64  `json:"head"`
}

func macKey(smk []byte) []byte {
	r := hkdf.New(sha256.New, smk, []byte("syncsh-remote-manifest"), []byte("mac-v1"))
	key := make([]byte, 32)
	_, _ = io.ReadFull(r, key)
	return key
}

func SignRemote(m *RemoteManifest, smk []byte) error {
	m.MAC = ""
	if m.UpdatedAt == 0 {
		m.UpdatedAt = time.Now().UnixMilli()
	}
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, macKey(smk))
	mac.Write(body)
	m.MAC = hex.EncodeToString(mac.Sum(nil))
	return nil
}

func VerifyRemote(m RemoteManifest, smk []byte) error {
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
		return fmt.Errorf("remote manifest mac mismatch")
	}
	if m.Version != CurrentVersion {
		return fmt.Errorf("unsupported remote version %d", m.Version)
	}
	return nil
}

func SignGeneration(m *generations.Manifest, smk []byte) error {
	return generations.Sign(m, smk)
}

func MustSMK(smk []byte) error {
	if len(smk) != envelope.SMKSize {
		return fmt.Errorf("invalid smk size")
	}
	return nil
}
