package terminal

import (
	"bytes"
	"testing"
)

func TestCreateRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := CreateRequest{Shell: "/bin/zsh", Cwd: "/tmp", Cols: 80, Rows: 24, Env: []string{"FOO=bar"}}
	if err := WriteCreate(&buf, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCreate(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Shell != want.Shell || got.Cwd != want.Cwd || got.Cols != 80 || got.Rows != 24 || len(got.Env) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, FrameData, []byte("abc")); err != nil {
		t.Fatal(err)
	}
	kind, payload, err := ReadFrame(&buf)
	if err != nil || kind != FrameData || string(payload) != "abc" {
		t.Fatalf("kind=%d payload=%q err=%v", kind, payload, err)
	}
}
