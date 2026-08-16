package ack

import (
	"encoding/json"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/sync/merge"
)

const CurrentVersion = 1

type File struct {
	Version      int            `json:"version"`
	DeviceID     string         `json:"device_id"`
	Frontier     merge.Frontier `json:"frontier"`
	CheckpointID string         `json:"checkpoint_id,omitempty"`
	UpdatedAt    int64          `json:"updated_at"`
}

func New(deviceID string, f merge.Frontier, checkpointID string) File {
	return File{
		Version:      CurrentVersion,
		DeviceID:     deviceID,
		Frontier:     merge.Clone(f),
		CheckpointID: checkpointID,
		UpdatedAt:    time.Now().UnixMilli(),
	}
}

func Encode(f File) ([]byte, error) { return json.Marshal(f) }

func Decode(b []byte) (File, error) {
	var f File
	err := json.Unmarshal(b, &f)
	return f, err
}
