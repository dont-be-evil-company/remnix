package rsync

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mistweaverco/syncsh/internal/transport"
	"github.com/mistweaverco/syncsh/internal/transport/directory"
)

type Transport struct {
	Remote string
	Work   string
	inner  *directory.Transport
	opened bool
}

func New(remote, work string) *Transport {
	return &Transport{Remote: remote, Work: work, inner: directory.New(work)}
}

func (t *Transport) Begin(ctx context.Context) error {
	if err := os.MkdirAll(t.Work, 0o700); err != nil {
		return err
	}
	_ = runRsync(ctx, ensureSlash(t.Remote), ensureSlash(t.Work))
	t.opened = true
	return nil
}

func (t *Transport) End(ctx context.Context) error {
	return runRsync(ctx, ensureSlash(t.Work), ensureSlash(t.Remote))
}

func (t *Transport) List(ctx context.Context, prefix string) ([]transport.Object, error) {
	return t.inner.List(ctx, prefix)
}
func (t *Transport) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return t.inner.Get(ctx, key)
}
func (t *Transport) Put(ctx context.Context, key string, r io.Reader) error {
	return t.inner.Put(ctx, key, r)
}
func (t *Transport) PutAtomic(ctx context.Context, key string, r io.Reader) error {
	return t.inner.PutAtomic(ctx, key, r)
}
func (t *Transport) Remove(ctx context.Context, key string) error {
	return t.inner.Remove(ctx, key)
}

func runRsync(ctx context.Context, src, dst string) error {
	cmd := exec.CommandContext(ctx, "rsync", "-a", "--exclude", "*.tmp-*", src, dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("rsync: %w (%s)", err, out)
	}
	return nil
}

func ensureSlash(p string) string {
	if p == "" {
		return p
	}
	if p[len(p)-1] == '/' {
		return p
	}
	return p + "/"
}

func WorkDir(base string) string {
	return filepath.Join(base, "rsync-stage")
}
