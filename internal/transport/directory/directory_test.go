package directory

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutAtomicGetList(t *testing.T) {
	tr := New(t.TempDir())
	ctx := context.Background()
	if err := tr.PutAtomic(ctx, "events/d1/a.bundle", bytes.NewReader([]byte("hello"))); err != nil {
		t.Fatal(err)
	}
	r, err := tr.Get(ctx, "events/d1/a.bundle")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(r)
	if buf.String() != "hello" {
		t.Fatalf("%q", buf.String())
	}
	objs, err := tr.List(ctx, "events/")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || objs[0].Key != "events/d1/a.bundle" {
		t.Fatalf("%+v", objs)
	}
}

func TestTmpFilesAreNotAuthoritative(t *testing.T) {
	root := t.TempDir()
	tr := New(root)
	ctx := context.Background()
	path := filepath.Join(root, "events", "x.bundle.tmp-crash")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	objs, err := tr.List(ctx, "events/")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range objs {
		if strings.Contains(o.Key, ".tmp") {
			t.Fatalf("listed tmp object %s", o.Key)
		}
	}
}

func TestRejectDotDot(t *testing.T) {
	tr := New(t.TempDir())
	if _, err := tr.Get(context.Background(), "../etc/passwd"); err == nil {
		t.Fatal("expected error")
	}
}

func TestMkdirAndHealth(t *testing.T) {
	root := t.TempDir()
	tr := New(root)
	ctx := context.Background()
	if err := tr.Mkdir(ctx, "events/d1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "events", "d1")); err != nil {
		t.Fatal(err)
	}
	st, err := tr.HealthCheck(ctx)
	if err != nil || st.State != "ok" {
		t.Fatalf("health %+v err=%v", st, err)
	}
	if !tr.Capabilities().AtomicRename {
		t.Fatal("directory should advertise atomic rename")
	}
}

func TestRemoveAllDeletesCheckpointDir(t *testing.T) {
	root := t.TempDir()
	tr := New(root)
	ctx := context.Background()
	if err := tr.PutAtomic(ctx, "checkpoints/old/manifest", bytes.NewReader([]byte("{}"))); err != nil {
		t.Fatal(err)
	}
	dirs, err := tr.ListDirs(ctx, "checkpoints/")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 || dirs[0] != "checkpoints/old" {
		t.Fatalf("dirs %v", dirs)
	}
	if err := tr.Remove(ctx, "checkpoints/old"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "checkpoints", "old")); !os.IsNotExist(err) {
		t.Fatal("directory should be gone")
	}
}
