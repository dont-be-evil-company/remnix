package bundle

import (
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/crypto/envelope"
	"github.com/dont-be-evil-company/remnix/internal/sync/event"
)

func TestPackUnpack(t *testing.T) {
	smk, _ := envelope.GenerateSMK()
	evs := []event.Event{
		{Version: 1, Type: event.TypeHistoryCreated, DeviceID: "d", Seq: 1, TimeUnix: 1, Payload: []byte("a")},
		{Version: 1, Type: event.TypeHistoryCreated, DeviceID: "d", Seq: 2, TimeUnix: 2, Payload: []byte("b")},
	}
	raw, h, err := Pack("d", "g1", evs, smk)
	if err != nil {
		t.Fatal(err)
	}
	if h.SeqStart != 1 || h.SeqEnd != 2 {
		t.Fatalf("%+v", h)
	}
	gotH, got, err := Unpack(raw, smk)
	if err != nil {
		t.Fatal(err)
	}
	if gotH.EventCount != 2 || len(got) != 2 {
		t.Fatalf("%+v %d", gotH, len(got))
	}
	peek, err := PeekHeader(raw)
	if err != nil || peek.SeqEnd != 2 || peek.GenerationID != "g1" {
		t.Fatalf("peek %+v %v", peek, err)
	}
	if Filename(raw) == "" {
		t.Fatal("filename")
	}
}
