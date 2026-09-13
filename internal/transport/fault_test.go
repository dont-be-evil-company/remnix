package transport

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

type memFS struct {
	objs map[string][]byte
}

func (m *memFS) List(_ context.Context, prefix string) ([]Object, error) {
	var out []Object
	for k, v := range m.objs {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			out = append(out, Object{Key: k, Size: int64(len(v))})
		}
	}
	return out, nil
}
func (m *memFS) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.objs[key])), nil
}
func (m *memFS) Put(_ context.Context, key string, r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if m.objs == nil {
		m.objs = map[string][]byte{}
	}
	m.objs[key] = b
	return nil
}
func (m *memFS) PutAtomic(ctx context.Context, key string, r io.Reader) error {
	return m.Put(ctx, key, r)
}
func (m *memFS) Remove(_ context.Context, key string) error {
	delete(m.objs, key)
	return nil
}
func (m *memFS) ListDirs(context.Context, string) ([]string, error) { return nil, nil }
func (m *memFS) Mkdir(context.Context, string) error                { return nil }
func (m *memFS) HealthCheck(context.Context) (HealthStatus, error) {
	return HealthStatus{State: HealthOK}, nil
}
func (m *memFS) Capabilities() Capabilities { return Capabilities{} }

func TestFaultTransportCorruptReadAndDelete(t *testing.T) {
	base := &memFS{objs: map[string][]byte{"a": []byte("hello")}}
	ft := &FaultTransport{Base: base, CorruptRead: true, FailDelete: true}
	rc, err := ft.Get(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	if string(got) == "hello" {
		t.Fatal("expected corrupted bytes")
	}
	if err := ft.Remove(context.Background(), "a"); err == nil {
		t.Fatal("expected delete fault")
	}
}

func TestFaultTransportStaleList(t *testing.T) {
	base := &memFS{objs: map[string][]byte{"a": []byte("1")}}
	ft := &FaultTransport{Base: base, StaleList: true}
	if _, err := ft.List(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	base.objs["b"] = []byte("2")
	objs, err := ft.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 {
		t.Fatalf("stale list should hide new object, got %d", len(objs))
	}
}

func TestFaultTransportPutKeyAndLostResponse(t *testing.T) {
	base := &memFS{objs: map[string][]byte{}}
	ctx := context.Background()
	ft := &FaultTransport{Base: base, FailPutKey: "metadata/manifest"}
	if err := ft.PutAtomic(ctx, "metadata/manifest", bytes.NewReader([]byte("x"))); err == nil {
		t.Fatal("expected put fault")
	}
	if _, ok := base.objs["metadata/manifest"]; ok {
		t.Fatal("failed put should not write")
	}
	ft.FailPutAfterWrite = true
	if err := ft.PutAtomic(ctx, "metadata/manifest", bytes.NewReader([]byte("y"))); err == nil {
		t.Fatal("expected lost-response fault")
	}
	if string(base.objs["metadata/manifest"]) != "y" {
		t.Fatalf("lost response should still commit write, got %q", base.objs["metadata/manifest"])
	}
}

func TestFaultTransportRemoveAfterN(t *testing.T) {
	base := &memFS{objs: map[string][]byte{"events/a": []byte("1"), "events/b": []byte("2")}}
	ctx := context.Background()
	ft := &FaultTransport{Base: base, FailRemovePrefix: "events/", FailRemoveAfterN: 1}
	if err := ft.Remove(ctx, "events/a"); err != nil {
		t.Fatal(err)
	}
	if err := ft.Remove(ctx, "events/b"); err == nil {
		t.Fatal("expected second delete to fail")
	}
	if _, ok := base.objs["events/a"]; ok {
		t.Fatal("first object should be gone")
	}
	if _, ok := base.objs["events/b"]; !ok {
		t.Fatal("second object should remain")
	}
}

func TestFaultTransportCancel(t *testing.T) {
	base := &memFS{objs: map[string][]byte{"a": []byte("1")}}
	ft := &FaultTransport{Base: base, Delay: 50 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ft.List(ctx, ""); err == nil {
		t.Fatal("expected cancel")
	}
}
