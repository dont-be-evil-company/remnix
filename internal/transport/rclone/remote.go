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
		base := path.Base(key)
		if strings.HasSuffix(key, ".tmp") || strings.Contains(base, ".tmp-") {
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
		base := path.Base(key)
		if strings.HasSuffix(key, ".tmp") || strings.Contains(base, ".tmp-") {
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
	if parent != "." && parent != "" {
		if err := operations.Mkdir(ctx, t.fs, parent); err != nil {
			return mapErr(err)
		}
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	final := key
	upload := key
	if atomic {
		upload = key + ".tmp-" + uuid.NewString()
	}
	src := object.NewStaticObjectInfo(upload, time.Now(), int64(len(data)), true, nil, t.fs)
	obj, err := t.fs.Put(ctx, bytes.NewReader(data), src)
	if err != nil {
		if obj != nil {
			_ = obj.Remove(ctx)
		}
		return mapErr(err)
	}
	if obj.Size() != int64(len(data)) {
		_ = obj.Remove(ctx)
		return fmt.Errorf("rclone put size mismatch: got %d want %d", obj.Size(), len(data))
	}
	if !atomic || upload == final {
		return nil
	}
	if move := t.fs.Features().Move; move != nil {
		if _, err := move(ctx, obj, final); err != nil {
			_ = obj.Remove(ctx)
			return mapErr(err)
		}
		return nil
	}
	finalSrc := object.NewStaticObjectInfo(final, time.Now(), int64(len(data)), true, nil, t.fs)
	finalObj, err := t.fs.Put(ctx, bytes.NewReader(data), finalSrc)
	_ = obj.Remove(ctx)
	if err != nil {
		if finalObj != nil {
			_ = finalObj.Remove(ctx)
		}
		return mapErr(err)
	}
	return nil
}

func (t *Transport) Mkdir(ctx context.Context, key string) error {
	return mapErr(operations.Mkdir(ctx, t.fs, strings.Trim(key, "/")))
}

func (t *Transport) Remove(ctx context.Context, key string) error {
	key = strings.Trim(key, "/")
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
