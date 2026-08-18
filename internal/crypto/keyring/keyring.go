package keyring

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	oskeyring "github.com/zalando/go-keyring"
)

const (
	service = "syncsh"
	version = 1
)

type Backend interface {
	Set(service, user, password string) error
	Get(service, user string) (string, error)
	Delete(service, user string) error
}

type osBackend struct{}

func (osBackend) Set(service, user, password string) error {
	return oskeyring.Set(service, user, password)
}
func (osBackend) Get(service, user string) (string, error) {
	return oskeyring.Get(service, user)
}
func (osBackend) Delete(service, user string) error {
	return oskeyring.Delete(service, user)
}

var (
	mu      sync.Mutex
	backend Backend = osBackend{}
)

func Use(b Backend) {
	mu.Lock()
	defer mu.Unlock()
	if b == nil {
		backend = osBackend{}
		return
	}
	backend = b
}

func current() Backend {
	mu.Lock()
	defer mu.Unlock()
	return backend
}

type bundle struct {
	Version int               `json:"version"`
	Keys    map[string][]byte `json:"keys"`
}

func account(deviceID string) string {
	return "smk-" + deviceID
}

func Set(deviceID string, smks map[string][]byte) error {
	if deviceID == "" {
		return fmt.Errorf("keyring: missing device id")
	}
	body, err := json.Marshal(bundle{Version: version, Keys: smks})
	if err != nil {
		return err
	}
	if err := current().Set(service, account(deviceID), string(body)); err != nil {
		return annotate(err)
	}
	return nil
}

func Get(deviceID string) (map[string][]byte, error) {
	if deviceID == "" {
		return nil, nil
	}
	s, err := current().Get(service, account(deviceID))
	if err != nil {
		if errors.Is(err, oskeyring.ErrNotFound) || isNotFound(err) {
			return nil, nil
		}
		return nil, annotate(err)
	}
	var b bundle
	if err := json.Unmarshal([]byte(s), &b); err != nil {
		return nil, fmt.Errorf("keyring: corrupt SMK entry: %w", err)
	}
	if b.Keys == nil {
		return map[string][]byte{}, nil
	}
	return b.Keys, nil
}

func Delete(deviceID string) error {
	if deviceID == "" {
		return nil
	}
	err := current().Delete(service, account(deviceID))
	if err != nil && !errors.Is(err, oskeyring.ErrNotFound) && !isNotFound(err) {
		return annotate(err)
	}
	return nil
}

func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") || strings.Contains(s, "cannot find")
}

func annotate(err error) error {
	if err == nil {
		return nil
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "could not find") ||
		strings.Contains(s, "secret service") ||
		strings.Contains(s, "no such") ||
		strings.Contains(s, "dbus") ||
		strings.Contains(s, "unsupported") {
		return fmt.Errorf("%w\n\nOS keyring is unavailable. Unlock GNOME Keyring, KWallet, KeePassXC, macOS Keychain, or Windows Credential Manager. The Sync Master Key is not written to a file", err)
	}
	return err
}

type Memory struct {
	mu sync.Mutex
	m  map[string]string
}

func NewMemory() *Memory {
	return &Memory{m: map[string]string{}}
}

func (m *Memory) key(service, user string) string { return service + "\x00" + user }

func (m *Memory) Set(service, user, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.m[m.key(service, user)] = password
	return nil
}

func (m *Memory) Get(service, user string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.m[m.key(service, user)]
	if !ok {
		return "", oskeyring.ErrNotFound
	}
	return s, nil
}

func (m *Memory) Delete(service, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.m, m.key(service, user))
	return nil
}

func Available() error {
	host, _ := os.Hostname()
	probe := account("probe-" + host)
	if err := current().Set(service, probe, "ok"); err != nil {
		return annotate(err)
	}
	_ = current().Delete(service, probe)
	return nil
}
