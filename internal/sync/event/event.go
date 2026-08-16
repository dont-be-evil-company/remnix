package event

import (
	"fmt"
	"time"

	"github.com/mistweaverco/syncsh/internal/cborx"
	"github.com/mistweaverco/syncsh/internal/history"
)

const CurrentVersion = 1

const (
	TypeHistoryCreated    = "history-created"
	TypeHistoryTombstoned = "history-tombstoned"
	TypeDeviceMeta        = "device-meta"
	TypeCheckpointRef     = "checkpoint-ref"
)

type Event struct {
	Version  int    `cbor:"1,keyasint"`
	Type     string `cbor:"2,keyasint"`
	DeviceID string `cbor:"3,keyasint"`
	Seq      int64  `cbor:"4,keyasint"`
	TimeUnix int64  `cbor:"5,keyasint"`
	Payload  []byte `cbor:"6,keyasint"`
}

type HistoryCreated struct {
	ID         string `cbor:"1,keyasint"`
	Command    string `cbor:"2,keyasint"`
	StartTS    int64  `cbor:"3,keyasint"`
	EndTS      int64  `cbor:"4,keyasint"`
	DurationMs int64  `cbor:"5,keyasint"`
	ExitStatus *int   `cbor:"6,keyasint"`
	Cwd        string `cbor:"7,keyasint"`
	SessionID  string `cbor:"8,keyasint"`
	Hostname   string `cbor:"9,keyasint"`
	Shell      string `cbor:"10,keyasint"`
}

type HistoryTombstoned struct {
	HistoryID      string `cbor:"1,keyasint"`
	OriginDeviceID string `cbor:"2,keyasint"`
	OriginSeq      int64  `cbor:"3,keyasint"`
}

type DeviceMeta struct {
	Name     string `cbor:"1,keyasint"`
	Hostname string `cbor:"2,keyasint"`
	Status   string `cbor:"3,keyasint"`
}

type CheckpointRef struct {
	CheckpointID string `cbor:"1,keyasint"`
}

func Encode(ev Event) ([]byte, error) {
	if ev.Version == 0 {
		ev.Version = CurrentVersion
	}
	return cborx.Marshal(ev)
}

func Decode(b []byte) (Event, error) {
	var ev Event
	if err := cborx.Unmarshal(b, &ev); err != nil {
		return Event{}, err
	}
	if ev.Version != CurrentVersion {
		return Event{}, fmt.Errorf("unsupported event version %d", ev.Version)
	}
	if ev.DeviceID == "" || ev.Seq <= 0 || ev.Type == "" {
		return Event{}, fmt.Errorf("malformed event")
	}
	return ev, nil
}

func NewHistoryCreated(deviceID string, seq int64, e history.Entry) (Event, error) {
	p := HistoryCreated{
		ID:        e.ID,
		Command:   e.Command,
		StartTS:   e.StartTS.UnixMilli(),
		Cwd:       e.Cwd,
		SessionID: e.SessionID,
		Hostname:  e.Hostname,
		Shell:     e.Shell,
	}
	if e.EndTS != nil {
		p.EndTS = e.EndTS.UnixMilli()
	}
	if e.DurationMs != nil {
		p.DurationMs = *e.DurationMs
	}
	p.ExitStatus = e.ExitStatus
	payload, err := cborx.Marshal(p)
	if err != nil {
		return Event{}, err
	}
	return Event{
		Version:  CurrentVersion,
		Type:     TypeHistoryCreated,
		DeviceID: deviceID,
		Seq:      seq,
		TimeUnix: time.Now().UnixMilli(),
		Payload:  payload,
	}, nil
}

func DecodeHistoryCreated(ev Event) (HistoryCreated, error) {
	var p HistoryCreated
	if err := cborx.Unmarshal(ev.Payload, &p); err != nil {
		return HistoryCreated{}, err
	}
	return p, nil
}

func NewHistoryTombstoned(deviceID string, seq int64, p HistoryTombstoned) (Event, error) {
	payload, err := cborx.Marshal(p)
	if err != nil {
		return Event{}, err
	}
	return Event{
		Version:  CurrentVersion,
		Type:     TypeHistoryTombstoned,
		DeviceID: deviceID,
		Seq:      seq,
		TimeUnix: time.Now().UnixMilli(),
		Payload:  payload,
	}, nil
}

func DecodeHistoryTombstoned(ev Event) (HistoryTombstoned, error) {
	var p HistoryTombstoned
	if err := cborx.Unmarshal(ev.Payload, &p); err != nil {
		return HistoryTombstoned{}, err
	}
	return p, nil
}
