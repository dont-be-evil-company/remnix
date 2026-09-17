package terminal

import (
	"bytes"
	"errors"
	"io"
	"syscall"
	"testing"
)

func TestWriteFullExact(t *testing.T) {
	w := &shortWriter{limit: 4096}
	want := bytes.Repeat([]byte("abc"), 100)
	if err := writeFull(w, want, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.buf, want) {
		t.Fatalf("got %d want %d", len(w.buf), len(want))
	}
}

func TestWriteFullOneByteAtATime(t *testing.T) {
	w := &shortWriter{limit: 1}
	want := []byte("hello, pty")
	if err := writeFull(w, want, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.buf, want) {
		t.Fatalf("got %q", w.buf)
	}
}

func TestWriteFullRandomShort(t *testing.T) {
	w := &shortWriter{limit: 3}
	want := bytes.Repeat([]byte{0x1b, '[', 'A'}, 50)
	if err := writeFull(w, want, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.buf, want) {
		t.Fatalf("len %d", len(w.buf))
	}
}

func TestWriteFullEINTR(t *testing.T) {
	w := &eintrWriter{rest: []byte("xyz"), left: 2}
	if err := writeFull(w, []byte("xyz"), nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(w.got, []byte("xyz")) {
		t.Fatalf("got %q", w.got)
	}
}

func TestWriteFullEAGAINThenComplete(t *testing.T) {
	w := &shortWriter{limit: 2, fail: errWouldBlock, blocked: 2}
	want := []byte("abcdef")
	waits := 0
	err := writeFull(w, want, func() error {
		waits++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if waits == 0 {
		t.Fatal("expected EAGAIN waits")
	}
	if !bytes.Equal(w.buf, want) {
		t.Fatalf("got %q", w.buf)
	}
}

func TestWriteFrameShortWrites(t *testing.T) {
	w := &shortWriter{limit: 1}
	payload := []byte("payload-bytes")
	if err := WriteFrame(w, FrameData, payload); err != nil {
		t.Fatal(err)
	}
	kind, got, err := ReadFrame(bytes.NewReader(w.buf))
	if err != nil || kind != FrameData || !bytes.Equal(got, payload) {
		t.Fatalf("kind=%d payload=%q err=%v", kind, got, err)
	}
}

func TestWriteFullClosedPeer(t *testing.T) {
	err := writeFull(zeroWriter{}, []byte("x"), nil)
	if !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("got %v", err)
	}
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }

type eintrWriter struct {
	rest []byte
	got  []byte
	left int
}

func (w *eintrWriter) Write(p []byte) (int, error) {
	if w.left > 0 {
		w.left--
		return 0, syscall.EINTR
	}
	n := 1
	if n > len(p) {
		n = len(p)
	}
	w.got = append(w.got, p[:n]...)
	return n, nil
}
