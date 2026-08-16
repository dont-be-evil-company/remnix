package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/mistweaverco/syncsh/internal/cborx"
	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
	"github.com/mistweaverco/syncsh/internal/sync/event"
)

const CurrentVersion = 1

type Header struct {
	Version      int    `cbor:"1,keyasint"`
	DeviceID     string `cbor:"2,keyasint"`
	SeqStart     int64  `cbor:"3,keyasint"`
	SeqEnd       int64  `cbor:"4,keyasint"`
	EventCount   int    `cbor:"5,keyasint"`
	GenerationID string `cbor:"6,keyasint"`
	Nonce        []byte `cbor:"7,keyasint"`
}

type File struct {
	Header     Header `cbor:"1,keyasint"`
	Ciphertext []byte `cbor:"2,keyasint"`
}

func Pack(deviceID, generationID string, events []event.Event, smk []byte) ([]byte, Header, error) {
	if len(events) == 0 {
		return nil, Header{}, fmt.Errorf("empty bundle")
	}
	payload, err := cborx.Marshal(events)
	if err != nil {
		return nil, Header{}, err
	}
	nonce, err := envelope.RandomNonce()
	if err != nil {
		return nil, Header{}, err
	}
	h := Header{
		Version:      CurrentVersion,
		DeviceID:     deviceID,
		SeqStart:     events[0].Seq,
		SeqEnd:       events[len(events)-1].Seq,
		EventCount:   len(events),
		GenerationID: generationID,
		Nonce:        nonce,
	}
	ad, err := cborx.Marshal(h)
	if err != nil {
		return nil, Header{}, err
	}
	ct, err := envelope.Seal(smk, nonce, payload, ad)
	if err != nil {
		return nil, Header{}, err
	}
	raw, err := cborx.Marshal(File{Header: h, Ciphertext: ct})
	if err != nil {
		return nil, Header{}, err
	}
	return raw, h, nil
}

func PeekHeader(raw []byte) (Header, error) {
	var f File
	if err := cborx.Unmarshal(raw, &f); err != nil {
		return Header{}, err
	}
	return f.Header, nil
}

func Unpack(raw, smk []byte) (Header, []event.Event, error) {
	var f File
	if err := cborx.Unmarshal(raw, &f); err != nil {
		return Header{}, nil, fmt.Errorf("bundle: %w", err)
	}
	if f.Header.Version != CurrentVersion {
		return Header{}, nil, fmt.Errorf("unsupported bundle version %d", f.Header.Version)
	}
	ad, err := cborx.Marshal(f.Header)
	if err != nil {
		return Header{}, nil, err
	}
	pt, err := envelope.Open(smk, f.Header.Nonce, f.Ciphertext, ad)
	if err != nil {
		return Header{}, nil, err
	}
	var events []event.Event
	if err := cborx.Unmarshal(pt, &events); err != nil {
		return Header{}, nil, fmt.Errorf("bundle payload: %w", err)
	}
	if len(events) != f.Header.EventCount {
		return Header{}, nil, fmt.Errorf("event count mismatch")
	}
	return f.Header, events, nil
}

func IntegrityID(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func Filename(raw []byte) string {
	return IntegrityID(raw) + ".bundle"
}

func EqualHeader(a, b Header) bool {
	return a.DeviceID == b.DeviceID && a.SeqStart == b.SeqStart && a.SeqEnd == b.SeqEnd && bytes.Equal(a.Nonce, b.Nonce)
}
