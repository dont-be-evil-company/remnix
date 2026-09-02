package rclone

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mistweaverco/syncsh/internal/transport"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/object"
	"github.com/rclone/rclone/fs/operations"
)

var _ transport.Transport = (*Transport)(nil)
var _ transport.Deduper = (*Transport)(nil)

type Transport struct {
	fs     fs.Fs
	remote string
	root   string
}

func Open(ctx context.Context, remoteName, root string) (*Transport, error) {
	mu.Lock()
	defer mu.Unlock()
	spec := remoteName + ":"
	root = strings.Trim(root, "/")
	if root != "" {
		spec += root
	}
	f, err := fs.NewFs(ctx, spec)
	if err != nil {
		return nil, mapErr(err)
	}
	return &Transport{fs: f, remote: remoteName, root: root}, nil
}

func OpenLocal(ctx context.Context, dir string) (*Transport, error) {
	mu.Lock()
	defer mu.Unlock()
	f, err := fs.NewFs(ctx, dir)
	if err != nil {
		return nil, mapErr(err)
	}
	return &Transport{fs: f, remote: "local", root: dir}, nil
}

func (t *Transport) RemoteName() string { return t.remote }
func (t *Transport) Root() string       { return t.root }

func (t *Transport) List(ctx context.Context, prefix string) ([]transport.Object, error) {
	prefix = strings.Trim(prefix, "/")
	var out []transport.Object
	err := walk(ctx, t.fs, prefix, func(o fs.Object) {
		if skipObject(o) {
			return
		}
		key := o.Remote()
		if isTempName(key) {
			return
		}
		out = append(out, transport.Object{Key: key, Size: o.Size()})
	})
	if err != nil && !errors.Is(err, fs.ErrorDirNotFound) {
		return nil, mapErr(err)
	}
	return out, nil
}

func (t *Transport) ListDirs(ctx context.Context, prefix string) ([]string, error) {
	prefix = strings.Trim(prefix, "/")
	entries, err := t.fs.List(ctx, prefix)
	if err != nil {
		if errors.Is(err, fs.ErrorDirNotFound) {
			return nil, nil
		}
		return nil, mapErr(err)
	}
	var out []string
	entries.ForDir(func(d fs.Directory) {
		name := d.Remote()
		if strings.Contains(path.Base(name), ".tmp-") {
			return
		}
		out = append(out, name)
	})
	return out, nil
}

func (t *Transport) ListShallow(ctx context.Context, prefix string) ([]transport.Object, error) {
	prefix = strings.Trim(prefix, "/")
	entries, err := t.fs.List(ctx, prefix)
	if err != nil {
		if errors.Is(err, fs.ErrorDirNotFound) {
			return nil, nil
		}
		return nil, mapErr(err)
	}
	var out []transport.Object
	entries.ForObject(func(o fs.Object) {
		if skipObject(o) {
			return
		}
		key := o.Remote()
		if isTempName(key) {
			return
		}
		out = append(out, transport.Object{Key: key, Size: o.Size()})
	})
	return out, nil
}

func (t *Transport) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := t.fs.NewObject(ctx, strings.Trim(key, "/"))
	if err != nil {
		return nil, mapErr(err)
	}
	if skipObject(obj) {
		return nil, fmt.Errorf("%w: skipping non-downloadable object %s", os.ErrNotExist, key)
	}
	rc, err := obj.Open(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return rc, nil
}

func (t *Transport) Put(ctx context.Context, key string, r io.Reader) error {
	return t.put(ctx, key, r, false)
}

func (t *Transport) PutAtomic(ctx context.Context, key string, r io.Reader) error {
	return t.put(ctx, key, r, true)
}

func (t *Transport) put(ctx context.Context, key string, r io.Reader, atomic bool) error {
	key = strings.Trim(key, "/")
	parent := path.Dir(key)
	// Drive Put already creates parents via FindPath. Extra Mkdir after a
	// dir-cache flush can spawn a second folder with the same name.
	if parent != "." && parent != "" && !t.allowsDuplicateNames() {
		if err := operations.Mkdir(ctx, t.fs, parent); err != nil {
			return mapErr(err)
		}
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	// Drive (and other duplicate-name backends) must not use tmp+rename.
	// A server-side rename leaves the old tmp name in local Drive mirrors.
	if t.allowsDuplicateNames() {
		obj, err := t.upload(ctx, key, data)
		if err != nil {
			return err
		}
		t.removeOthers(ctx, key, obj)
		t.removeTempsFor(ctx, key)
		return nil
	}
	final := key
	upload := key
	if atomic {
		upload = key + ".tmp-" + uuid.NewString()
	}
	obj, err := t.upload(ctx, upload, data)
	if err != nil {
		return err
	}
	if !atomic || upload == final {
		return nil
	}
	if _, err := t.rename(ctx, obj, final, data); err != nil {
		_ = obj.Remove(ctx)
		return err
	}
	return nil
}

func (t *Transport) upload(ctx context.Context, key string, data []byte) (fs.Object, error) {
	src := object.NewStaticObjectInfo(key, time.Now(), int64(len(data)), true, nil, t.fs)
	obj, err := t.fs.Put(ctx, bytes.NewReader(data), src)
	if err != nil {
		if obj != nil {
			_ = obj.Remove(ctx)
		}
		return nil, mapErr(err)
	}
	if obj.Size() != int64(len(data)) {
		_ = obj.Remove(ctx)
		return nil, fmt.Errorf("rclone put size mismatch: got %d want %d", obj.Size(), len(data))
	}
	return obj, nil
}

func (t *Transport) rename(ctx context.Context, obj fs.Object, final string, data []byte) (fs.Object, error) {
	if move := t.fs.Features().Move; move != nil {
		moved, err := move(ctx, obj, final)
		if err != nil {
			return nil, mapErr(err)
		}
		if moved != nil {
			return moved, nil
		}
		return obj, nil
	}
	finalSrc := object.NewStaticObjectInfo(final, time.Now(), int64(len(data)), true, nil, t.fs)
	finalObj, err := t.fs.Put(ctx, bytes.NewReader(data), finalSrc)
	_ = obj.Remove(ctx)
	if err != nil {
		if finalObj != nil {
			_ = finalObj.Remove(ctx)
		}
		return nil, mapErr(err)
	}
	return finalObj, nil
}

// Dedupe deletes extra objects that share a path, leftover `.tmp-*` files,
// and duplicate directories. Google Drive allows the same name more than
// once; rclone Feature Move only renames, so acks and device metadata used
// to accumulate copies, and Mkdir after a dir-cache flush used to spawn a
// second `checkpoints/` folder that GC could not see.
func (t *Transport) Dedupe(ctx context.Context) error {
	t.flushDirCache()
	if err := t.mergeDuplicateDirs(ctx, ""); err != nil {
		return err
	}
	t.flushDirCache()
	for _, dir := range []string{"acks", "metadata", "keys", "events", "checkpoints"} {
		if err := t.dedupeTree(ctx, dir); err != nil {
			return err
		}
	}
	return nil
}

func (t *Transport) allowsDuplicateNames() bool {
	return t.fs.Features().DuplicateFiles
}

func (t *Transport) flushDirCache() {
	if flush := t.fs.Features().DirCacheFlush; flush != nil {
		flush()
	}
}

func (t *Transport) mergeDuplicateDirs(ctx context.Context, dir string) error {
	merge := t.fs.Features().MergeDirs
	if merge == nil {
		return nil
	}
	entries, err := t.fs.List(ctx, dir)
	if err != nil {
		if errors.Is(err, fs.ErrorDirNotFound) {
			return nil
		}
		return mapErr(err)
	}
	byName := map[string][]fs.Directory{}
	entries.ForDir(func(d fs.Directory) {
		if strings.Contains(path.Base(d.Remote()), ".tmp-") {
			return
		}
		byName[d.Remote()] = append(byName[d.Remote()], d)
	})
	merged := false
	for _, ds := range byName {
		if len(ds) < 2 {
			continue
		}
		if err := merge(ctx, ds); err != nil {
			return mapErr(err)
		}
		merged = true
	}
	if merged {
		t.flushDirCache()
	}
	return nil
}

func (t *Transport) dedupeTree(ctx context.Context, dir string) error {
	if err := t.mergeDuplicateDirs(ctx, dir); err != nil {
		return err
	}
	entries, err := t.fs.List(ctx, dir)
	if err != nil {
		if errors.Is(err, fs.ErrorDirNotFound) {
			return nil
		}
		return mapErr(err)
	}
	byName := map[string][]fs.Object{}
	var dirs []string
	entries.ForObject(func(o fs.Object) {
		if skipObject(o) {
			return
		}
		if isTempName(o.Remote()) {
			_ = o.Remove(ctx)
			return
		}
		if t.allowsDuplicateNames() {
			byName[o.Remote()] = append(byName[o.Remote()], o)
		}
	})
	seen := map[string]bool{}
	entries.ForDir(func(d fs.Directory) {
		if strings.Contains(path.Base(d.Remote()), ".tmp-") {
			return
		}
		name := d.Remote()
		if seen[name] {
			return
		}
		seen[name] = true
		dirs = append(dirs, name)
	})
	for _, objs := range byName {
		if len(objs) < 2 {
			continue
		}
		keep := newestObject(objs)
		for _, o := range objs {
			if sameObject(o, keep) {
				continue
			}
			_ = o.Remove(ctx)
		}
	}
	for _, d := range dirs {
		if err := t.dedupeTree(ctx, d); err != nil {
			return err
		}
	}
	return nil
}

func (t *Transport) objectsNamed(ctx context.Context, key string) []fs.Object {
	key = strings.Trim(key, "/")
	parent := path.Dir(key)
	if parent == "." {
		parent = ""
	}
	entries, err := t.fs.List(ctx, parent)
	if err != nil {
		return nil
	}
	var out []fs.Object
	entries.ForObject(func(o fs.Object) {
		if skipObject(o) {
			return
		}
		if o.Remote() == key {
			out = append(out, o)
		}
	})
	return out
}

func (t *Transport) removeOthers(ctx context.Context, key string, keep fs.Object) {
	if !t.allowsDuplicateNames() {
		return
	}
	for _, o := range t.objectsNamed(ctx, key) {
		if sameObject(o, keep) {
			continue
		}
		_ = o.Remove(ctx)
	}
}

func (t *Transport) removeTempsFor(ctx context.Context, key string) {
	key = strings.Trim(key, "/")
	parent := path.Dir(key)
	if parent == "." {
		parent = ""
	}
	prefix := key + ".tmp-"
	entries, err := t.fs.List(ctx, parent)
	if err != nil {
		return
	}
	entries.ForObject(func(o fs.Object) {
		if skipObject(o) {
			return
		}
		if strings.HasPrefix(o.Remote(), prefix) {
			_ = o.Remove(ctx)
		}
	})
}

func (t *Transport) Mkdir(ctx context.Context, key string) error {
	return mapErr(operations.Mkdir(ctx, t.fs, strings.Trim(key, "/")))
}

func (t *Transport) Remove(ctx context.Context, key string) error {
	key = strings.Trim(key, "/")
	if t.allowsDuplicateNames() {
		objs := t.objectsNamed(ctx, key)
		if len(objs) > 0 {
			var firstErr error
			for _, o := range objs {
				if err := o.Remove(ctx); err != nil && firstErr == nil {
					firstErr = err
				}
			}
			return mapErr(firstErr)
		}
	}
	obj, err := t.fs.NewObject(ctx, key)
	if err == nil {
		return mapErr(obj.Remove(ctx))
	}
	if !errors.Is(err, fs.ErrorObjectNotFound) && !errors.Is(err, fs.ErrorIsDir) {
		if !errors.Is(err, fs.ErrorDirNotFound) {
			mapped := mapErr(err)
			if !errors.Is(mapped, os.ErrNotExist) {
				return mapped
			}
		}
	}
	if purge := t.fs.Features().Purge; purge != nil {
		err := purge(ctx, key)
		if errors.Is(err, fs.ErrorDirNotFound) || errors.Is(err, fs.ErrorObjectNotFound) {
			return nil
		}
		return mapErr(err)
	}
	err = walkRemove(ctx, t.fs, key)
	if errors.Is(err, fs.ErrorDirNotFound) {
		return nil
	}
	if err != nil {
		return mapErr(err)
	}
	return mapErr(t.fs.Rmdir(ctx, key))
}

func (t *Transport) HealthCheck(ctx context.Context) (transport.HealthStatus, error) {
	// Never List the remote root - Drive listings of My Drive look like a hang.
	if about := t.fs.Features().About; about != nil {
		if _, err := about(ctx); err != nil {
			return transport.ClassifyHealth(mapErr(err)), nil
		}
		return transport.HealthStatus{State: transport.HealthOK, Message: "ok"}, nil
	}
	_, err := t.fs.NewObject(ctx, ".syncsh-health-check")
	if err != nil && !errors.Is(err, fs.ErrorObjectNotFound) && !errors.Is(err, fs.ErrorDirNotFound) {
		st := transport.ClassifyHealth(mapErr(err))
		return st, nil
	}
	return transport.HealthStatus{State: transport.HealthOK, Message: "ok"}, nil
}

func (t *Transport) Capabilities() transport.Capabilities {
	feat := t.fs.Features()
	name := t.fs.Name()
	local := t.remote == "local" || name == "local" || name == "memory"
	return transport.Capabilities{
		AtomicRename:     feat.Move != nil,
		Mkdir:            true,
		Rename:           feat.Move != nil || feat.Copy != nil,
		RecursiveDelete:  feat.Purge != nil,
		VirtualDirs:      feat.BucketBased,
		ListingExpensive: !local,
	}
}

func walk(ctx context.Context, f fs.Fs, dir string, fn func(fs.Object)) error {
	entries, err := f.List(ctx, dir)
	if err != nil {
		return err
	}
	entries.ForObject(fn)
	var walkErr error
	entries.ForDir(func(d fs.Directory) {
		if walkErr != nil {
			return
		}
		if err := walk(ctx, f, d.Remote(), fn); err != nil && !errors.Is(err, fs.ErrorDirNotFound) {
			walkErr = err
		}
	})
	return walkErr
}

func skipObject(o fs.Object) bool {
	if o == nil {
		return true
	}
	// Google Docs / sheets have size -1 and fail with alt=media downloads.
	if o.Size() < 0 {
		return true
	}
	return false
}

func isTempName(key string) bool {
	base := path.Base(key)
	return strings.HasSuffix(base, ".tmp") || strings.Contains(base, ".tmp-")
}

func objectID(o fs.Object) string {
	type ider interface{ ID() string }
	if o == nil {
		return ""
	}
	if x, ok := o.(ider); ok {
		return x.ID()
	}
	return ""
}

func sameObject(a, b fs.Object) bool {
	if a == nil || b == nil {
		return false
	}
	if a == b {
		return true
	}
	idA, idB := objectID(a), objectID(b)
	return idA != "" && idA == idB
}

func newestObject(objs []fs.Object) fs.Object {
	keep := objs[0]
	for _, o := range objs[1:] {
		if o.ModTime(context.Background()).After(keep.ModTime(context.Background())) {
			keep = o
		}
	}
	return keep
}

func walkRemove(ctx context.Context, f fs.Fs, dir string) error {
	entries, err := f.List(ctx, dir)
	if err != nil {
		return err
	}
	var remErr error
	entries.ForObject(func(o fs.Object) {
		if remErr != nil {
			return
		}
		remErr = o.Remove(ctx)
	})
	if remErr != nil {
		return remErr
	}
	entries.ForDir(func(d fs.Directory) {
		if remErr != nil {
			return
		}
		if err := walkRemove(ctx, f, d.Remote()); err != nil {
			remErr = err
			return
		}
		remErr = f.Rmdir(ctx, d.Remote())
	})
	return remErr
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, fs.ErrorObjectNotFound), errors.Is(err, fs.ErrorDirNotFound):
		return fmt.Errorf("%w: %v", os.ErrNotExist, err)
	case errors.Is(err, fs.ErrorPermissionDenied):
		return fmt.Errorf("%w: %v", transport.ErrPermissionDenied, err)
	default:
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "unauthenticated") || strings.Contains(msg, "unauthorized") || strings.Contains(msg, "oauth") || strings.Contains(msg, "expired") && strings.Contains(msg, "token") {
			return fmt.Errorf("%w: %v", transport.ErrAuthRequired, err)
		}
		if strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") || strings.Contains(msg, "network is unreachable") || strings.Contains(msg, "terminated signal") {
			return fmt.Errorf("%w: %v", transport.ErrOffline, err)
		}
		return err
	}
}
