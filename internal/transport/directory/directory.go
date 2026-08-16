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
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
