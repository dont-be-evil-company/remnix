package event

import (
	"testing"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/history"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	ev := Event{Version: 1, Type: TypeHistoryCreated, DeviceID: "d", Seq: 1, TimeUnix: 2, Payload: []byte{1, 2, 3}}
	b, err := Encode(ev)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != ev.Type || got.Seq != ev.Seq || got.DeviceID != ev.DeviceID {
		t.Fatalf("%+v", got)
	}
}

func TestDecodeRejectsMalformed(t *testing.T) {
	if _, err := Decode([]byte("not-cbor")); err == nil {
		t.Fatal("expected error")
	}
	b, _ := Encode(Event{Version: 1, Type: "", DeviceID: "d", Seq: 1})
	if _, err := Decode(b); err == nil {
		t.Fatal("expected malformed")
	}
}

func TestHistoryCreatedInvalidUTF8(t *testing.T) {
	cmd := "printf \x80"
	ev, err := NewHistoryCreated("d", 1, history.Entry{ID: "h1", Command: cmd, StartTS: time.Unix(1, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	p, err := DecodeHistoryCreated(ev)
	if err != nil {
		t.Fatal(err)
	}
	if p.Command != cmd {
		t.Fatalf("%q", p.Command)
	}
}
