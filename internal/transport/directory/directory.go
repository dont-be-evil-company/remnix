package directory

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/mistweaverco/syncsh/internal/transport"
)

var _ transport.Transport = (*Transport)(nil)

type Transport struct {
	Root string
}

func New(root string) *Transport {
	return &Transport{Root: root}
}

func (t *Transport) resolve(key string) (string, error) {
	clean := filepath.Clean("/" + key)
	clean = strings.TrimPrefix(clean, "/")
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("invalid key %q", key)
	}
	return filepath.Join(t.Root, filepath.FromSlash(clean)), nil
}

func (t *Transport) List(_ context.Context, prefix string) ([]transport.Object, error) {
	root := t.Root
	if prefix != "" {
		p, err := t.resolve(prefix)
		if err != nil {
			return nil, err
		}
		root = p
	}
	var out []transport.Object
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(t.Root, path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(rel, ".tmp") || strings.Contains(filepath.Base(rel), ".tmp-") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, transport.Object{Key: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}

func (t *Transport) ListShallow(_ context.Context, prefix string) ([]transport.Object, error) {
	root := t.Root
	base := strings.TrimSuffix(strings.TrimPrefix(prefix, "/"), "/")
	if prefix != "" {
		p, err := t.resolve(prefix)
		if err != nil {
			return nil, err
		}
		root = p
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []transport.Object
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".tmp") || strings.Contains(name, ".tmp-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		key := name
		if base != "" {
			key = base + "/" + name
		}
		out = append(out, transport.Object{Key: filepath.ToSlash(key), Size: info.Size()})
	}
	return out, nil
}

func (t *Transport) Get(_ context.Context, key string) (io.ReadCloser, error) {
	p, err := t.resolve(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (t *Transport) Put(ctx context.Context, key string, r io.Reader) error {
	return t.put(key, r, false)
}

func (t *Transport) PutAtomic(ctx context.Context, key string, r io.Reader) error {
	return t.put(key, r, true)
}

func (t *Transport) put(key string, r io.Reader, atomic bool) error {
	p, err := t.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp-" + uuid.NewString()
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if atomic {
		if err := os.Rename(tmp, p); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return nil
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (t *Transport) Remove(_ context.Context, key string) error {
	p, err := t.resolve(key)
	if err != nil {
		return err
	}
	err = os.RemoveAll(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (t *Transport) ListDirs(_ context.Context, prefix string) ([]string, error) {
	root := t.Root
	if prefix != "" {
		p, err := t.resolve(prefix)
		if err != nil {
			return nil, err
		}
		root = p
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	base := strings.TrimSuffix(strings.TrimPrefix(prefix, "/"), "/")
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".tmp") || strings.Contains(name, ".tmp-") {
			continue
		}
		key := name
		if base != "" {
			key = base + "/" + name
		}
		out = append(out, filepath.ToSlash(key))
	}
	return out, nil
}

func (t *Transport) Mkdir(_ context.Context, key string) error {
	p, err := t.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(p, 0o700); err != nil {
		return transport.MapFSError(err)
	}
	return nil
}

func (t *Transport) HealthCheck(ctx context.Context) (transport.HealthStatus, error) {
	if t.Root == "" {
		return transport.HealthStatus{State: transport.HealthMisconfigured, Message: "directory path is empty"}, nil
	}
	if err := os.MkdirAll(t.Root, 0o700); err != nil {
		st := transport.ClassifyHealth(transport.MapFSError(err))
		return st, nil
	}
	if _, err := t.List(ctx, ""); err != nil {
		st := transport.ClassifyHealth(transport.MapFSError(err))
		return st, nil
	}
	return transport.HealthStatus{State: transport.HealthOK, Message: "ok"}, nil
}

func (t *Transport) Capabilities() transport.Capabilities {
	return transport.Capabilities{
		AtomicRename:    true,
		Mkdir:           true,
		Rename:          true,
		RecursiveDelete: true,
		VirtualDirs:     false,
	}
}
