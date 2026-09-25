package terminal

import (
	"bytes"
	"testing"
)

func TestCreateRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := CreateRequest{Shell: "/bin/zsh", Cwd: "/tmp", Cols: 80, Rows: 24, Xpixel: 1920, Ypixel: 1080, Env: []string{"FOO=bar"}}
	if err := WriteCreate(&buf, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCreate(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Shell != want.Shell || got.Cwd != want.Cwd || got.Cols != 80 || got.Rows != 24 || got.Xpixel != 1920 || got.Ypixel != 1080 || len(got.Env) != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestCreateWithoutPixels(t *testing.T) {
	raw := "CREATE\nshell=/bin/sh\ncwd=/tmp\ncols=80\nrows=24\nenv_count=0\n\n"
	got, err := ReadCreate(bytes.NewBufferString(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.Cols != 80 || got.Rows != 24 || got.Xpixel != 0 || got.Ypixel != 0 {
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
	if err := WriteFrame(&buf, FrameRaw, []byte("\x1b[?1049l")); err != nil {
		t.Fatal(err)
	}
	kind, payload, err = ReadFrame(&buf)
	if err != nil || kind != FrameRaw || string(payload) != "\x1b[?1049l" {
		t.Fatalf("raw kind=%d payload=%q err=%v", kind, payload, err)
	}
}
