package checkpoint

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mistweaverco/syncsh/internal/sync/merge"
)

const CurrentVersion = 1

type Manifest struct {
	Version      int            `json:"version"`
	ID           string         `json:"id"`
	GenerationID string         `json:"generation_id"`
	Frontier     merge.Frontier `json:"frontier"`
	CreatedAt    int64          `json:"created_at"`
}

func NewManifest(id, generationID string, f merge.Frontier) Manifest {
	return Manifest{
		Version:      CurrentVersion,
		ID:           id,
		GenerationID: generationID,
		Frontier:     merge.Clone(f),
		CreatedAt:    time.Now().UnixMilli(),
	}
}

func EncodeManifest(m Manifest) ([]byte, error) { return json.Marshal(m) }

func DecodeManifest(b []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, err
	}
	if m.Version != CurrentVersion {
		return Manifest{}, fmt.Errorf("unsupported checkpoint version %d", m.Version)
	}
	return m, nil
}
