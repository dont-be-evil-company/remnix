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
// renames, and several objects or directories may share a path.
type dupFs struct {
	files      []*dupObj
	dirs       []*dupDir
	mkdirCalls []string
	features   *fs.Features
}

func newDupFs() *dupFs {
	f := &dupFs{}
	f.features = &fs.Features{
		DuplicateFiles: true,
		Move:           f.Move,
		MergeDirs:      f.MergeDirs,
	}
	return f
}

func (f *dupFs) Name() string             { return "dup" }
func (f *dupFs) Root() string             { return "" }
func (f *dupFs) String() string           { return "dupfs" }
func (f *dupFs) Precision() time.Duration { return time.Nanosecond }
func (f *dupFs) Hashes() hash.Set         { return hash.NewHashSet() }
func (f *dupFs) Features() *fs.Features   { return f.features }
func (f *dupFs) Mkdir(_ context.Context, dir string) error {
	f.mkdirCalls = append(f.mkdirCalls, dir)
	return nil
}
func (f *dupFs) Rmdir(context.Context, string) error { return nil }

func (f *dupFs) dirID(remote string) string {
	if remote == "" {
		return ""
	}
	for _, d := range f.dirs {
		if d.remote == remote {
			return d.id
		}
	}
	return ""
}

func (f *dupFs) List(_ context.Context, dir string) (fs.DirEntries, error) {
	if len(f.dirs) > 0 {
		return f.listByParent(dir), nil
	}
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

func (f *dupFs) listByParent(dir string) fs.DirEntries {
	parentID := f.dirID(dir)
	var entries fs.DirEntries
	for _, d := range f.dirs {
		if d.parent == parentID {
			entries = append(entries, d)
		}
	}
	for _, o := range f.files {
		if o.parent == parentID {
			entries = append(entries, o)
		}
	}
	return entries
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

func (f *dupFs) MergeDirs(_ context.Context, dirs []fs.Directory) error {
	if len(dirs) < 2 {
		return nil
	}
	keepID := dirs[0].ID()
	drop := map[string]bool{}
	for _, d := range dirs[1:] {
		drop[d.ID()] = true
	}
	for _, d := range f.dirs {
		if drop[d.parent] {
			d.parent = keepID
		}
	}
	for _, o := range f.files {
		if drop[o.parent] {
			o.parent = keepID
		}
	}
	kept := f.dirs[:0]
	for _, d := range f.dirs {
		if !drop[d.id] {
			kept = append(kept, d)
		}
	}
	f.dirs = kept
	return nil
}

type dupDir struct {
	f      *dupFs
	id     string
	remote string
	parent string
}

func (d *dupDir) Fs() fs.Info                       { return d.f }
func (d *dupDir) String() string                    { return d.remote }
func (d *dupDir) Remote() string                    { return d.remote }
func (d *dupDir) ModTime(context.Context) time.Time { return time.Time{} }
func (d *dupDir) Size() int64                       { return -1 }
func (d *dupDir) Items() int64                      { return -1 }
func (d *dupDir) ID() string                        { return d.id }

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
	parent  string
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
	_ fs.Fs        = (*dupFs)(nil)
	_ fs.Object    = (*dupObj)(nil)
	_ fs.Directory = (*dupDir)(nil)
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

func TestPutAtomicLeavesNoTempFiles(t *testing.T) {
	f := newDupFs()
	tr := &Transport{fs: f, remote: "dup"}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		body := fmt.Sprintf("frontier-%d", i)
		if err := tr.PutAtomic(ctx, "acks/dev.ack", bytes.NewReader([]byte(body))); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range f.files {
		if strings.Contains(o.remote, ".tmp-") {
			t.Fatalf("leftover temp file %q", o.remote)
		}
	}
}

func TestDedupeDeletesTempFiles(t *testing.T) {
	f := newDupFs()
	tr := &Transport{fs: f, remote: "dup"}
	ctx := context.Background()
	ack := object.NewStaticObjectInfo("acks/dev.ack", time.Now(), 3, true, nil, f)
	if _, err := f.Put(ctx, bytes.NewReader([]byte("ack")), ack); err != nil {
		t.Fatal(err)
	}
	tmpName := "acks/dev.ack.tmp-" + uuid.NewString()
	tmp := object.NewStaticObjectInfo(tmpName, time.Now(), 3, true, nil, f)
	if _, err := f.Put(ctx, bytes.NewReader([]byte("tmp")), tmp); err != nil {
		t.Fatal(err)
	}
	if len(f.files) != 2 {
		t.Fatalf("setup %d", len(f.files))
	}
	if err := tr.Dedupe(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.files) != 1 {
		t.Fatalf("after dedupe %d", len(f.files))
	}
	if f.files[0].remote != "acks/dev.ack" {
		t.Fatalf("kept %q", f.files[0].remote)
	}
}

func TestPutAtomicCleansExistingTemps(t *testing.T) {
	f := newDupFs()
	tr := &Transport{fs: f, remote: "dup"}
	ctx := context.Background()
	tmpName := "acks/dev.ack.tmp-" + uuid.NewString()
	tmp := object.NewStaticObjectInfo(tmpName, time.Now(), 3, true, nil, f)
	if _, err := f.Put(ctx, bytes.NewReader([]byte("tmp")), tmp); err != nil {
		t.Fatal(err)
	}
	if err := tr.PutAtomic(ctx, "acks/dev.ack", bytes.NewReader([]byte("ack"))); err != nil {
		t.Fatal(err)
	}
	for _, o := range f.files {
		if strings.Contains(o.remote, ".tmp-") {
			t.Fatalf("leftover temp file %q", o.remote)
		}
	}
	if len(f.files) != 1 || f.files[0].remote != "acks/dev.ack" {
		t.Fatalf("files=%d remote=%v", len(f.files), f.files)
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

func TestDedupeMergesDuplicateCheckpointParents(t *testing.T) {
	f := newDupFs()
	tr := &Transport{fs: f, remote: "dup"}
	ctx := context.Background()
	a := &dupDir{f: f, id: "A", remote: "checkpoints", parent: ""}
	b := &dupDir{f: f, id: "B", remote: "checkpoints", parent: ""}
	old := &dupDir{f: f, id: "old", remote: "checkpoints/old", parent: "A"}
	neu := &dupDir{f: f, id: "new", remote: "checkpoints/new", parent: "B"}
	f.dirs = []*dupDir{a, b, old, neu}
	f.files = []*dupObj{
		{f: f, id: "m-old", remote: "checkpoints/old/manifest", parent: "old", data: []byte("old")},
		{f: f, id: "m-new", remote: "checkpoints/new/manifest", parent: "new", data: []byte("new")},
	}

	root, err := f.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	nCheckpoints := 0
	root.ForDir(func(d fs.Directory) {
		if d.Remote() == "checkpoints" {
			nCheckpoints++
		}
	})
	if nCheckpoints != 2 {
		t.Fatalf("setup checkpoints parents: %d", nCheckpoints)
	}

	if err := tr.Dedupe(ctx); err != nil {
		t.Fatal(err)
	}

	root, err = f.List(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	nCheckpoints = 0
	root.ForDir(func(d fs.Directory) {
		if d.Remote() == "checkpoints" {
			nCheckpoints++
		}
	})
	if nCheckpoints != 1 {
		t.Fatalf("after dedupe checkpoints parents: %d", nCheckpoints)
	}

	children, err := f.List(ctx, "checkpoints")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	children.ForDir(func(d fs.Directory) {
		got[path.Base(d.Remote())] = true
	})
	if !got["old"] || !got["new"] {
		t.Fatalf("merged children %v", got)
	}
}

func TestPutAtomicSkipsMkdirOnDuplicateNameBackend(t *testing.T) {
	f := newDupFs()
	tr := &Transport{fs: f, remote: "dup"}
	if err := tr.PutAtomic(context.Background(), "checkpoints/c1/manifest", bytes.NewReader([]byte("{}"))); err != nil {
		t.Fatal(err)
	}
	if len(f.mkdirCalls) != 0 {
		t.Fatalf("Mkdir on Drive-like backend: %v", f.mkdirCalls)
	}
}
