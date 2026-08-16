package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/config"
)

const (
	PhaseConfig = "config"
	PhaseKeys   = "keys"
	PhaseRemote = "remote"
)

type State struct {
	Phase        string `json:"phase"`
	DeviceID     string `json:"device_id"`
	DeviceName   string `json:"device_name"`
	GenerationID string `json:"generation_id,omitempty"`
	UpdatedAt    int64  `json:"updated_at"`
}

func LoadState() (*State, error) {
	b, err := os.ReadFile(config.SetupStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("setup-state: %w", err)
	}
	return &st, nil
}

func (s *State) Save() error {
	if s == nil {
		return nil
	}
	s.UpdatedAt = time.Now().Unix()
	if err := os.MkdirAll(config.DataDir(), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(config.SetupStatePath(), b, 0o600)
}

func ClearState() error {
	err := os.Remove(config.SetupStatePath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
