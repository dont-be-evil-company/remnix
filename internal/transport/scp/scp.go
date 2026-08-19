package scp

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/mistweaverco/syncsh/internal/transport"
	"github.com/mistweaverco/syncsh/internal/transport/directory"
)

var (
	_ transport.Transport = (*Transport)(nil)
	_ transport.Session   = (*Transport)(nil)
)

type Transport struct {
	Host  string
	User  string
	Path  string
	Port  int
	Work  string
	inner *directory.Transport
}

func New(host, user, remotePath, work string, port int) *Transport {
	return &Transport{Host: host, User: user, Path: remotePath, Port: port, Work: work, inner: directory.New(work)}
}

func (t *Transport) spec() string {
	host := t.Host
	if t.User != "" {
		host = t.User + "@" + host
	}
	return host + ":" + t.Path
}

func (t *Transport) scpArgs(src, dst string) []string {
	args := []string{"-r", "-q"}
	if t.Port > 0 {
		args = append(args, "-P", strconv.Itoa(t.Port))
	}
	args = append(args, src, dst)
	return args
}

func (t *Transport) Begin(ctx context.Context) error {
	if err := os.MkdirAll(t.Work, 0o700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "scp", t.scpArgs(t.spec()+"/.", t.Work)...)
	_ = cmd.Run()
	return nil
}

func (t *Transport) End(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "scp", t.scpArgs(t.Work+"/.", t.spec())...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("scp push: %w (%s)", err, out)
	}
	return nil
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
func (t *Transport) ListDirs(ctx context.Context, prefix string) ([]string, error) {
	return t.inner.ListDirs(ctx, prefix)
}
func (t *Transport) ListShallow(ctx context.Context, prefix string) ([]transport.Object, error) {
	return t.inner.ListShallow(ctx, prefix)
}
func (t *Transport) Mkdir(ctx context.Context, key string) error {
	return t.inner.Mkdir(ctx, key)
}
func (t *Transport) HealthCheck(ctx context.Context) (transport.HealthStatus, error) {
	if t.Host == "" || t.Path == "" {
		return transport.HealthStatus{State: transport.HealthMisconfigured, Message: "scp host or path is empty"}, nil
	}
	return t.inner.HealthCheck(ctx)
}
func (t *Transport) Capabilities() transport.Capabilities {
	return t.inner.Capabilities()
}

func WorkDir(base string) string {
	return filepath.Join(base, "scp-stage")
}
