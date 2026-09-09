package syncer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/crypto/envelope"
	"github.com/dont-be-evil-company/remnix/internal/crypto/generations"
	"golang.org/x/crypto/hkdf"
)

const CurrentVersion = 1

type RemoteManifest struct {
	Version          int      `json:"version"`
	Counter          int64    `json:"counter"`
	ActiveGeneration string   `json:"active_generation"`
	Devices          []string `json:"devices"`
	Retired          []string `json:"retired"`
	Pruned           []string `json:"pruned,omitempty"`
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
	r := hkdf.New(sha256.New, smk, []byte("remnix-remote-manifest"), []byte("mac-v1"))
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

func uniqueSorted(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
