package checkpoint

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mistweaverco/syncsh/internal/cborx"
	"github.com/mistweaverco/syncsh/internal/crypto/envelope"
	"github.com/mistweaverco/syncsh/internal/history"
	"github.com/mistweaverco/syncsh/internal/sync/merge"
)

func TestSnapshotRoundTrip(t *testing.T) {
	smk, err := envelope.GenerateSMK()
	if err != nil {
		t.Fatal(err)
	}
	entries := []history.Entry{{
		ID:      "1",
		Command: "ls",
		StartTS: time.Unix(1, 0).UTC(),
	}}
	nonce, ct, err := PackSnapshot(entries, smk)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnpackSnapshot(smk, nonce, ct)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != "ls" {
		t.Fatalf("%+v", got)
	}
	file := EncodeFile(nonce, ct)
	n2, c2, err := DecodeFile(file)
	if err != nil {
		t.Fatal(err)
	}
	got, err = UnpackSnapshot(smk, n2, c2)
	if err != nil || len(got) != 1 || got[0].Command != "ls" {
		t.Fatalf("file wrap: %+v %v", got, err)
	}
	m := NewManifest("c1", "g1", merge.Frontier{"d": 3})
	b, err := EncodeManifest(m)
	if err != nil {
		t.Fatal(err)
	}
	back, err := DecodeManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	if back.Frontier["d"] != 3 {
		t.Fatalf("%+v", back)
	}
}

func TestSnapshotRoundTripInvalidUTF8(t *testing.T) {
	smk, err := envelope.GenerateSMK()
	if err != nil {
		t.Fatal(err)
	}
	cmd := "echo \x80\xff"
	entries := []history.Entry{{
		ID:      "1",
		Command: cmd,
		StartTS: time.Unix(1, 0).UTC(),
		Cwd:     "/tmp/\x80",
	}}
	nonce, ct, err := PackSnapshot(entries, smk)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnpackSnapshot(smk, nonce, ct)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Command != cmd || got[0].Cwd != entries[0].Cwd {
		t.Fatalf("%+v", got)
	}
}

func TestSnapshotStaysCompact(t *testing.T) {
	cmds := []string{"ls", "git status", "cd ~/projects/personal/syncsh", "make test", "vim README.md"}
	entries := make([]history.Entry, 5000)
	for i := range entries {
		seq := int64(i + 1)
		entries[i] = history.Entry{
			ID:             fmt.Sprintf("01a00815-65ae-7f07-a449-%012d", i),
			Command:        fmt.Sprintf("%s #%d", cmds[i%len(cmds)], i%80),
			StartTS:        time.Unix(int64(1_700_000_000+i*3), 0).UTC(),
			Cwd:            "/home/marco/projects/personal/syncsh",
			SessionID:      fmt.Sprintf("sess-%d", i/40),
			Hostname:       "bonobo",
			DeviceID:       "01a00814-7f94-7c97-86c4-5f62fbfc2f14",
			Shell:          "zsh",
			OriginDeviceID: "01a00814-7f94-7c97-86c4-5f62fbfc2f14",
			OriginSeq:      &seq,
			CreatedAt:      time.Unix(int64(1_700_000_000+i*3), 0).UTC(),
		}
	}
	smk, err := envelope.GenerateSMK()
	if err != nil {
		t.Fatal(err)
	}
	nonce, ct, err := PackSnapshot(entries, smk)
	if err != nil {
		t.Fatal(err)
	}
	file := EncodeFile(nonce, ct)
	if len(file) > 200*1024 {
		t.Fatalf("snapshot file %d bytes", len(file))
	}
	got, err := UnpackSnapshot(smk, nonce, ct)
	if err != nil || len(got) != len(entries) || got[0].Command != entries[0].Command {
		t.Fatalf("roundtrip: n=%d err=%v", len(got), err)
	}
}

func TestDecodeFileAcceptsJSON(t *testing.T) {
	smk, err := envelope.GenerateSMK()
	if err != nil {
		t.Fatal(err)
	}
	entries := []history.Entry{{
		ID:      "1",
		Command: "ls",
		StartTS: time.Unix(1, 0).UTC(),
	}}
	nonce, ct, err := PackSnapshot(entries, smk)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := json.Marshal(jsonSnapshotFile{Nonce: nonce, Ciphertext: ct})
	if err != nil {
		t.Fatal(err)
	}
	n2, c2, err := DecodeFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnpackSnapshot(smk, n2, c2)
	if err != nil || len(got) != 1 || got[0].Command != "ls" {
		t.Fatalf("json wrapper: %+v %v", got, err)
	}
	n3, c3, err := DecodeFile([]byte(`{"nonce":"YQ==","ciphertext":"Yg=="}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(n3) != "a" || string(c3) != "b" {
		t.Fatalf("base64 json: nonce=%q ct=%q", n3, c3)
	}
}

func TestDecodeFileRejectsGarbage(t *testing.T) {
	if _, _, err := DecodeFile([]byte(`{}`)); err == nil {
		t.Fatal("expected error for empty json object")
	}
	if _, _, err := DecodeFile([]byte("not-a-snapshot")); err == nil {
		t.Fatal("expected error for random bytes")
	}
}

func TestUnpackJSONEntries(t *testing.T) {
	smk, err := envelope.GenerateSMK()
	if err != nil {
		t.Fatal(err)
	}
	entries := []history.Entry{{
		ID:      "1",
		Command: "ls",
		StartTS: time.Unix(1, 0).UTC(),
	}}
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := envelope.RandomNonce()
	if err != nil {
		t.Fatal(err)
	}
	ct, err := envelope.Seal(smk, nonce, raw, []byte("syncsh-checkpoint"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnpackSnapshot(smk, nonce, ct)
	if err != nil || len(got) != 1 || got[0].Command != "ls" {
		t.Fatalf("json entries: %+v %v", got, err)
	}
	gz, err := gzipBest(raw)
	if err != nil {
		t.Fatal(err)
	}
	ct, err = envelope.Seal(smk, nonce, gz, []byte("syncsh-checkpoint"))
	if err != nil {
		t.Fatal(err)
	}
	got, err = UnpackSnapshot(smk, nonce, ct)
	if err != nil || len(got) != 1 || got[0].Command != "ls" {
		t.Fatalf("gzip json entries: %+v %v", got, err)
	}
}

func TestUnpackV1CBOREntries(t *testing.T) {
	smk, err := envelope.GenerateSMK()
	if err != nil {
		t.Fatal(err)
	}
	entries := []history.Entry{{
		ID:      "1",
		Command: "ls",
		StartTS: time.Unix(1, 0).UTC(),
	}}
	raw, err := cborx.Marshal(snapshotV1{Version: 1, Entries: entries})
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := envelope.RandomNonce()
	if err != nil {
		t.Fatal(err)
	}
	ct, err := envelope.Seal(smk, nonce, raw, []byte("syncsh-checkpoint"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := UnpackSnapshot(smk, nonce, ct)
	if err != nil || len(got) != 1 || got[0].Command != "ls" {
		t.Fatalf("v1 cbor: %+v %v", got, err)
	}
	gz, err := gzipBest(raw)
	if err != nil {
		t.Fatal(err)
	}
	ct, err = envelope.Seal(smk, nonce, gz, []byte("syncsh-checkpoint"))
	if err != nil {
		t.Fatal(err)
	}
	file, err := json.Marshal(jsonSnapshotFile{Nonce: nonce, Ciphertext: ct})
	if err != nil {
		t.Fatal(err)
	}
	n2, c2, err := DecodeFile(file)
	if err != nil {
		t.Fatal(err)
	}
	got, err = UnpackSnapshot(smk, n2, c2)
	if err != nil || len(got) != 1 || got[0].Command != "ls" {
		t.Fatalf("v1 gzip json wrap: %+v %v", got, err)
	}
}

func TestEncodeFileStaysBinary(t *testing.T) {
	file := EncodeFile([]byte("n"), []byte("c"))
	if string(file[:4]) != snapshotMagic {
		t.Fatalf("EncodeFile should write %s, got %q", snapshotMagic, file[:4])
	}
}
