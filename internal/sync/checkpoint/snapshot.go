package checkpoint

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"time"

	"github.com/mistweaverco/syncsh/internal/cborx"
	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
	"github.com/mistweaverco/syncsh/internal/history"
)

const (
	snapshotVersion = 2
	snapshotMagic   = "SScp"
	snapshotFileV1  = 1
)

type snapshot struct {
	Version  int          `cbor:"1,keyasint"`
	Sessions []string     `cbor:"2,keyasint"`
	Hosts    []string     `cbor:"3,keyasint"`
	Devices  []string     `cbor:"4,keyasint"`
	Shells   []string     `cbor:"5,keyasint"`
	Rows     []compactRow `cbor:"6,keyasint"`
}

type compactRow struct {
	_            struct{} `cbor:",toarray"`
	ID           string
	Command      string
	StartMS      int64
	EndMS        *int64
	DurationMs   *int64
	ExitStatus   *int
	Cwd          string
	SessionIdx   int
	HostIdx      int
	DeviceIdx    int
	ShellIdx     int
	Deleted      bool
	OriginDevIdx int
	OriginSeq    *int64
	CreatedMS    int64
}

type intern struct {
	index map[string]int
	list  []string
}

func newIntern() *intern {
	return &intern{index: map[string]int{"": 0}, list: []string{""}}
}

func (in *intern) add(s string) int {
	if i, ok := in.index[s]; ok {
		return i
	}
	i := len(in.list)
	in.index[s] = i
	in.list = append(in.list, s)
	return i
}

func encodeSnapshot(entries []history.Entry) snapshot {
	sessions, hosts, devices, shells := newIntern(), newIntern(), newIntern(), newIntern()
	rows := make([]compactRow, len(entries))
	for i, e := range entries {
		var endMS *int64
		if e.EndTS != nil {
			v := e.EndTS.UnixMilli()
			endMS = &v
		}
		rows[i] = compactRow{
			ID:           e.ID,
			Command:      e.Command,
			StartMS:      e.StartTS.UnixMilli(),
			EndMS:        endMS,
			DurationMs:   e.DurationMs,
			ExitStatus:   e.ExitStatus,
			Cwd:          e.Cwd,
			SessionIdx:   sessions.add(e.SessionID),
			HostIdx:      hosts.add(e.Hostname),
			DeviceIdx:    devices.add(e.DeviceID),
			ShellIdx:     shells.add(e.Shell),
			Deleted:      e.Deleted,
			OriginDevIdx: devices.add(e.OriginDeviceID),
			OriginSeq:    e.OriginSeq,
			CreatedMS:    e.CreatedAt.UnixMilli(),
		}
	}
	return snapshot{
		Version:  snapshotVersion,
		Sessions: sessions.list,
		Hosts:    hosts.list,
		Devices:  devices.list,
		Shells:   shells.list,
		Rows:     rows,
	}
}

func (s snapshot) entries() ([]history.Entry, error) {
	at := func(list []string, i int) (string, error) {
		if i < 0 || i >= len(list) {
			return "", fmt.Errorf("snapshot intern index %d out of range", i)
		}
		return list[i], nil
	}
	out := make([]history.Entry, len(s.Rows))
	for i, r := range s.Rows {
		session, err := at(s.Sessions, r.SessionIdx)
		if err != nil {
			return nil, err
		}
		host, err := at(s.Hosts, r.HostIdx)
		if err != nil {
			return nil, err
		}
		dev, err := at(s.Devices, r.DeviceIdx)
		if err != nil {
			return nil, err
		}
		origin, err := at(s.Devices, r.OriginDevIdx)
		if err != nil {
			return nil, err
		}
		shell, err := at(s.Shells, r.ShellIdx)
		if err != nil {
			return nil, err
		}
		e := history.Entry{
			ID:             r.ID,
			Command:        r.Command,
			StartTS:        time.UnixMilli(r.StartMS).UTC(),
			DurationMs:     r.DurationMs,
			ExitStatus:     r.ExitStatus,
			Cwd:            r.Cwd,
			SessionID:      session,
			Hostname:       host,
			DeviceID:       dev,
			Shell:          shell,
			Deleted:        r.Deleted,
			OriginDeviceID: origin,
			OriginSeq:      r.OriginSeq,
			CreatedAt:      time.UnixMilli(r.CreatedMS).UTC(),
		}
		if r.EndMS != nil {
			t := time.UnixMilli(*r.EndMS).UTC()
			e.EndTS = &t
		}
		out[i] = e
	}
	return out, nil
}

func gzipBest(p []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if _, err := w.Write(p); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gunzip(p []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(p))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func PackSnapshot(entries []history.Entry, smk []byte) (nonce, ct []byte, err error) {
	raw, err := cborx.Marshal(encodeSnapshot(entries))
	if err != nil {
		return nil, nil, err
	}
	gz, err := gzipBest(raw)
	if err != nil {
		return nil, nil, err
	}
	nonce, err = envelope.RandomNonce()
	if err != nil {
		return nil, nil, err
	}
	ct, err = envelope.Seal(smk, nonce, gz, []byte("syncsh-checkpoint"))
	return nonce, ct, err
}

func UnpackSnapshot(smk, nonce, ct []byte) ([]history.Entry, error) {
	pt, err := envelope.Open(smk, nonce, ct, []byte("syncsh-checkpoint"))
	if err != nil {
		return nil, err
	}
	pt, err = gunzip(pt)
	if err != nil {
		return nil, err
	}
	var s snapshot
	if err := cborx.Unmarshal(pt, &s); err != nil {
		return nil, err
	}
	if s.Version != snapshotVersion {
		return nil, fmt.Errorf("unsupported snapshot version %d", s.Version)
	}
	return s.entries()
}

func EncodeFile(nonce, ct []byte) []byte {
	buf := make([]byte, 0, 4+1+1+len(nonce)+len(ct))
	buf = append(buf, snapshotMagic...)
	buf = append(buf, snapshotFileV1)
	buf = append(buf, byte(len(nonce)))
	buf = append(buf, nonce...)
	buf = append(buf, ct...)
	return buf
}

func DecodeFile(b []byte) (nonce, ct []byte, err error) {
	if len(b) < 6 || string(b[:4]) != snapshotMagic {
		return nil, nil, fmt.Errorf("not a syncsh snapshot")
	}
	if b[4] != snapshotFileV1 {
		return nil, nil, fmt.Errorf("unsupported snapshot file version %d", b[4])
	}
	nlen := int(b[5])
	if nlen < 1 || 6+nlen > len(b) {
		return nil, nil, fmt.Errorf("truncated snapshot file")
	}
	return b[6 : 6+nlen], b[6+nlen:], nil
}
