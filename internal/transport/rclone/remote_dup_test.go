package rclone

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/hash"
	"github.com/rclone/rclone/fs/object"
)

// dupFs mimics Google Drive: Put always creates a new object, Move only
// renames, and several objects may share a path.
type dupFs struct {
	files    []*dupObj
	features *fs.Features
}

func newDupFs() *dupFs {
	f := &dupFs{}
	f.features = &fs.Features{
		DuplicateFiles: true,
		Move:           f.Move,
	}
	return f
}

func (f *dupFs) Name() string                        { return "dup" }
func (f *dupFs) Root() string                        { return "" }
func (f *dupFs) String() string                      { return "dupfs" }
func (f *dupFs) Precision() time.Duration            { return time.Nanosecond }
func (f *dupFs) Hashes() hash.Set                    { return hash.NewHashSet() }
func (f *dupFs) Features() *fs.Features              { return f.features }
func (f *dupFs) Mkdir(context.Context, string) error { return nil }
func (f *dupFs) Rmdir(context.Context, string) error { return nil }

func (f *dupFs) List(_ context.Context, dir string) (fs.DirEntries, error) {
	var entries fs.DirEntries
	seenDir := map[string]bool{}
	prefix := dir
	if prefix != "" {
		prefix += "/"
	}
	for _, o := range f.files {
		if dir != "" && o.remote != dir && !strings.HasPrefix(o.remote, prefix) {
			continue
		}
		rest := o.remote
		if dir != "" {
			if o.remote == dir {
				continue
			}
			rest = strings.TrimPrefix(o.remote, prefix)
		}
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			full := rest[:i]
			if dir != "" {
				full = dir + "/" + rest[:i]
			}
			if !seenDir[full] {
				seenDir[full] = true
				entries = append(entries, fs.NewDir(full, time.Time{}))
			}
			continue
		}
		entries = append(entries, o)
	}
	return entries, nil
}

func (f *dupFs) NewObject(_ context.Context, remote string) (fs.Object, error) {
	for _, o := range f.files {
		if o.remote == remote {
			return o, nil
		}
	}
	return nil, fs.ErrorObjectNotFound
}

func (f *dupFs) Put(_ context.Context, in io.Reader, src fs.ObjectInfo, _ ...fs.OpenOption) (fs.Object, error) {
	data, err := io.ReadAll(in)
	if err != nil {
		return nil, err
	}
	o := &dupObj{
		f:       f,
		id:      uuid.NewString(),
		remote:  src.Remote(),
		data:    data,
		modTime: src.ModTime(context.Background()),
	}
	f.files = append(f.files, o)
	return o, nil
}

func (f *dupFs) Move(_ context.Context, src fs.Object, remote string) (fs.Object, error) {
	o, ok := src.(*dupObj)
	if !ok {
		return nil, fs.ErrorCantMove
	}
	o.remote = remote
	// New wrapper, same id - Drive's Move does this.
	return &dupObj{f: f, id: o.id, remote: o.remote, data: o.data, modTime: o.modTime}, nil
}

type dupObj struct {
	f       *dupFs
	id      string
	remote  string
	data    []byte
	modTime time.Time
}

func (o *dupObj) Fs() fs.Info                       { return o.f }
func (o *dupObj) String() string                    { return o.remote }
func (o *dupObj) Remote() string                    { return o.remote }
func (o *dupObj) ID() string                        { return o.id }
func (o *dupObj) ModTime(context.Context) time.Time { return o.modTime }
func (o *dupObj) Size() int64                       { return int64(len(o.data)) }
func (o *dupObj) Storable() bool                    { return true }
func (o *dupObj) Hash(context.Context, hash.Type) (string, error) {
	return "", hash.ErrUnsupported
}
func (o *dupObj) SetModTime(_ context.Context, t time.Time) error {
	o.modTime = t
	return nil
}
func (o *dupObj) Open(context.Context, ...fs.OpenOption) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(o.data)), nil
}
func (o *dupObj) Update(_ context.Context, in io.Reader, _ fs.ObjectInfo, _ ...fs.OpenOption) error {
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	o.data = data
	o.modTime = time.Now()
	return nil
}
func (o *dupObj) Remove(context.Context) error {
	files := o.f.files[:0]
	for _, x := range o.f.files {
		if x.id != o.id {
			files = append(files, x)
		}
	}
	o.f.files = files
	return nil
}

var (
	_ fs.Fs     = (*dupFs)(nil)
	_ fs.Object = (*dupObj)(nil)
)

func TestPutAtomicOverwritesDuplicateNames(t *testing.T) {
	f := newDupFs()
	tr := &Transport{fs: f, remote: "dup"}
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		body := fmt.Sprintf("frontier-%d", i)
		if err := tr.PutAtomic(ctx, "acks/dev.ack", bytes.NewReader([]byte(body))); err != nil {
			t.Fatal(err)
		}
	}
	named := 0
	for _, o := range f.files {
		if o.remote == "acks/dev.ack" {
			named++
		}
	}
	if named != 1 {
		t.Fatalf("duplicate ack objects: %d (files=%d)", named, len(f.files))
	}
	r, err := tr.Get(ctx, "acks/dev.ack")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, _ := io.ReadAll(r)
	if string(got) != "frontier-4" {
		t.Fatalf("got %q", got)
	}
}

func TestPutAtomicOverwritesDeviceMetadataDuplicates(t *testing.T) {
	f := newDupFs()
	tr := &Transport{fs: f, remote: "dup"}
	ctx := context.Background()
	key := path.Join("metadata", "devices", "dev.json")
	for i := 0; i < 3; i++ {
		if err := tr.PutAtomic(ctx, key, bytes.NewReader([]byte(fmt.Sprintf(`{"n":%d}`, i)))); err != nil {
			t.Fatal(err)
		}
	}
	named := 0
	for _, o := range f.files {
		if o.remote == key {
			named++
		}
	}
	if named != 1 {
		t.Fatalf("duplicate device objects: %d", named)
	}
}

func TestDedupeCollapsesDuplicateNames(t *testing.T) {
	f := newDupFs()
	tr := &Transport{fs: f, remote: "dup"}
	ctx := context.Background()
	info := object.NewStaticObjectInfo("acks/dev.ack", time.Now(), 1, true, nil, f)
	for i := 0; i < 4; i++ {
		body := fmt.Sprintf("%d", i)
		info = object.NewStaticObjectInfo("acks/dev.ack", time.Now().Add(time.Duration(i)*time.Second), int64(len(body)), true, nil, f)
		if _, err := f.Put(ctx, bytes.NewReader([]byte(body)), info); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.files) != 4 {
		t.Fatalf("setup %d", len(f.files))
	}
	if err := tr.Dedupe(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.files) != 1 {
		t.Fatalf("after dedupe %d", len(f.files))
	}
	if string(f.files[0].data) != "3" {
		t.Fatalf("kept %q, want newest", f.files[0].data)
	}
}

func TestRemoveDeletesAllDuplicateNames(t *testing.T) {
	f := newDupFs()
	tr := &Transport{fs: f, remote: "dup"}
	ctx := context.Background()
	info := object.NewStaticObjectInfo("acks/dev.ack", time.Now(), 1, true, nil, f)
	for i := 0; i < 3; i++ {
		if _, err := f.Put(ctx, bytes.NewReader([]byte("x")), info); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.Remove(ctx, "acks/dev.ack"); err != nil {
		t.Fatal(err)
	}
	if len(f.files) != 0 {
		t.Fatalf("leftover %d", len(f.files))
	}
}
