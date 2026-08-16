package protocol

import (
	"bytes"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	req := Request{Version: Version, ID: 7, Op: OpPing}
	var buf bytes.Buffer
	if err := WriteFrame(&buf, req); err != nil {
		t.Fatal(err)
	}
	var got Request
	if err := ReadFrame(&buf, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 7 || got.Op != OpPing {
		t.Fatalf("%+v", got)
	}
}
