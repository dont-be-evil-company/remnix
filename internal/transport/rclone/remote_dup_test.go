package rclone

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mistweaverco/syncsh/internal/transport"
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
	rootID     string
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
		return f.rootID
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

func (f *dupFs) Command(_ context.Context, name string, arg []string, _ map[string]string) (any, error) {
	if name != "query" || len(arg) != 1 {
		return nil, fs.ErrorCommandNotFound
	}
	want := driveQueryName(arg[0])
	var out []map[string]any
	for _, d := range f.dirs {
		if path.Base(d.remote) != want {
			continue
		}
		parents := []string{}
		if d.parent != "" {
			parents = []string{d.parent}
		}
		out = append(out, map[string]any{
			"id":      d.id,
			"name":    path.Base(d.remote),
			"parents": parents,
		})
	}
	return out, nil
}

func driveQueryName(q string) string {
	const p = "name='"
	i := strings.Index(q, p)
	if i < 0 {
		return ""
	}
	q = q[i+len(p):]
	j := strings.IndexByte(q, '\'')
	if j < 0 {
		return ""
	}
	return q[:j]
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
func (d *dupDir) ParentID() string                  { return d.parent }

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
func (o *dupObj) ParentID() string                  { return o.parent }
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

func TestMkdirBucketBasedCreatesKeepObject(t *testing.T) {
	f := newDupFs()
	f.features.BucketBased = true
	tr := &Transport{fs: f, remote: "s3"}
	if err := tr.Mkdir(context.Background(), "syncsh"); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, o := range f.files {
		if o.remote == "syncsh/"+dirKeepName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("keep object missing, files=%v", f.files)
	}
	dirs, err := tr.ListDirs(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, d := range dirs {
		if d == "syncsh" || strings.HasSuffix(d, "/syncsh") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("prefix not listable: %v", dirs)
	}
}

func TestHealthCheckBucketBasedListsRoot(t *testing.T) {
	inner := newDupFs()
	f := &bucketHealthFs{dupFs: inner}
	inner.features.BucketBased = true
	tr := &Transport{fs: f, remote: "s3"}
	st, err := tr.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != transport.HealthOK {
		t.Fatalf("health %+v", st)
	}
	if f.newObject != 0 {
		t.Fatalf("NewObject called %d times (S3 empty-key HeadObject)", f.newObject)
	}
	if f.listRoot != 1 {
		t.Fatalf("List root called %d times", f.listRoot)
	}
}

func TestHealthCheckEmptyRootListAllMyBucketsDenied(t *testing.T) {
	inner := newDupFs()
	f := &bucketHealthFs{
		dupFs:   inner,
		listErr: fmt.Errorf("operation error S3: ListBuckets, https response error StatusCode: 403, api error AccessDenied: User: arn:aws:iam::1:user/x is not authorized to perform: s3:ListAllMyBuckets"),
	}
	inner.features.BucketBased = true
	tr := &Transport{fs: f, remote: "s3"}
	st, err := tr.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != transport.HealthOK {
		t.Fatalf("scoped IAM should not fail account ListBuckets: %+v", st)
	}
	if f.newObject != 0 {
		t.Fatalf("NewObject called %d times", f.newObject)
	}
}

func TestHealthCheckRootedAccessDeniedStillFails(t *testing.T) {
	inner := newDupFs()
	f := &bucketHealthFs{
		dupFs:   inner,
		listErr: fmt.Errorf("operation error S3: ListObjectsV2, api error AccessDenied: Access Denied"),
	}
	inner.features.BucketBased = true
	tr := &Transport{fs: f, remote: "s3", root: "my-bucket"}
	st, err := tr.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.State != transport.HealthPermissionDenied {
		t.Fatalf("ListObjects denial inside a bucket: %+v", st)
	}
	if st.Action == "" {
		t.Fatal("expected IAM prefix hint")
	}
}

func TestListBucketDenied(t *testing.T) {
	err := fmt.Errorf("operation error S3: ListObjectsV2, api error AccessDenied: User: arn:aws:iam::1:user/x is not authorized to perform: s3:ListBucket on resource: \"arn:aws:s3:::bucket\"")
	if !listBucketDenied(err) {
		t.Fatal("expected ListBucket denial")
	}
	if listBucketDenied(fmt.Errorf("s3:ListAllMyBuckets")) {
		t.Fatal("account listing is not a bucket ListObjects denial")
	}
}

func TestMapErrAccessDenied(t *testing.T) {
	err := mapErr(fmt.Errorf("operation error S3: ListObjectsV2, api error AccessDenied: not authorized to perform: s3:ListBucket"))
	if !errors.Is(err, transport.ErrPermissionDenied) {
		t.Fatalf("got %v", err)
	}
}

func TestListAllBucketsDenied(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("s3:ListAllMyBuckets"), true},
		{fmt.Errorf("does not have storage.buckets.list access"), true},
		{fmt.Errorf("operation error S3: ListBuckets, api error AccessDenied"), true},
		{fmt.Errorf("operation error S3: ListObjectsV2, api error AccessDenied"), false},
		{fmt.Errorf("connection refused"), false},
	}
	for _, tc := range cases {
		if got := listAllBucketsDenied(tc.err); got != tc.want {
			t.Errorf("listAllBucketsDenied(%v)=%v want %v", tc.err, got, tc.want)
		}
	}
}

type bucketHealthFs struct {
	*dupFs
	newObject int
	listRoot  int
	listErr   error
}

func (f *bucketHealthFs) List(ctx context.Context, dir string) (fs.DirEntries, error) {
	if dir == "" {
		f.listRoot++
		if f.listErr != nil {
			return nil, f.listErr
		}
	}
	return f.dupFs.List(ctx, dir)
}

func (f *bucketHealthFs) NewObject(ctx context.Context, remote string) (fs.Object, error) {
	f.newObject++
	return nil, fmt.Errorf("operation error S3: HeadObject, serialization failed: serialization failed: input member Key must not be empty")
}

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
	for i := 0; i < 4; i++ {
		body := fmt.Sprintf("%d", i)
		info := object.NewStaticObjectInfo("acks/dev.ack", time.Now().Add(time.Duration(i)*time.Second), int64(len(body)), true, nil, f)
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

func TestDedupeMergesDuplicateRepoRootFolders(t *testing.T) {
	f := newDupFs()
	f.rootID = "live"
	f.features.Command = f.Command
	live := &dupDir{f: f, id: "live", remote: "syncsh", parent: "drive-root"}
	stale := &dupDir{f: f, id: "stale", remote: "syncsh", parent: "drive-root"}
	ckLive := &dupDir{f: f, id: "ck-live", remote: "checkpoints", parent: "live"}
	ckStale := &dupDir{f: f, id: "ck-stale", remote: "checkpoints", parent: "stale"}
	old := &dupDir{f: f, id: "old", remote: "checkpoints/old", parent: "ck-stale"}
	neu := &dupDir{f: f, id: "new", remote: "checkpoints/new", parent: "ck-live"}
	f.dirs = []*dupDir{live, stale, ckLive, ckStale, old, neu}
	tr := &Transport{fs: f, remote: "dup", root: "syncsh"}

	root, err := f.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	root.ForDir(func(d fs.Directory) {
		if d.Remote() == "checkpoints" {
			n++
		}
	})
	if n != 1 {
		t.Fatalf("live repo should hide sibling checkpoints: %d", n)
	}

	if err := tr.Dedupe(context.Background()); err != nil {
		t.Fatal(err)
	}

	nSyncsh := 0
	for _, d := range f.dirs {
		if d.remote == "syncsh" {
			nSyncsh++
		}
	}
	if nSyncsh != 1 {
		t.Fatalf("syncsh folders after dedupe: %d", nSyncsh)
	}

	children, err := f.List(context.Background(), "checkpoints")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	children.ForDir(func(d fs.Directory) {
		got[path.Base(d.Remote())] = true
	})
	if !got["old"] || !got["new"] {
		t.Fatalf("merged checkpoint dirs %v", got)
	}
}
