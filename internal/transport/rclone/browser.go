package rclone

import (
	"context"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/mistweaverco/syncsh/internal/tui/picker"
)

type BrowserFS struct {
	tr  *Transport
	cwd string
}

func NewBrowser(tr *Transport) *BrowserFS {
	return &BrowserFS{tr: tr, cwd: ""}
}

func (b *BrowserFS) Current() string { return b.cwd }
func (b *BrowserFS) SetCurrent(p string) {
	b.cwd = strings.Trim(p, "/")
}
func (b *BrowserFS) Clean(p string) string { return path.Clean(p) }
func (b *BrowserFS) Join(elem ...string) string {
	return path.Join(elem...)
}
func (b *BrowserFS) Parent(p string) string {
	p = strings.Trim(p, "/")
	if p == "" || p == "." {
		return ""
	}
	d := path.Dir(p)
	if d == "." {
		return ""
	}
	return d
}
func (b *BrowserFS) Abs(p string) (string, error) { return p, nil }
func (b *BrowserFS) Capabilities() picker.Capabilities {
	c := b.tr.Capabilities()
	return picker.Capabilities{Mkdir: c.Mkdir, Rename: c.Rename, Remove: c.RecursiveDelete, Expensive: c.ListingExpensive}
}

func (b *BrowserFS) List(ctx context.Context, p string) ([]picker.Entry, error) {
	dirs, err := b.tr.ListDirs(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make([]picker.Entry, 0, len(dirs))
	for _, d := range dirs {
		name := path.Base(d)
		out = append(out, picker.Entry{Name: name, Path: d, IsDir: true, DisplaySuffix: "/"})
	}
	return out, nil
}

func (b *BrowserFS) Mkdir(ctx context.Context, p string) error {
	return b.tr.Mkdir(ctx, p)
}

func (b *BrowserFS) Rename(ctx context.Context, oldPath, newPath string) error {
	if !b.tr.Capabilities().Rename {
		return fmt.Errorf("rename is not supported (or is expensive) on this backend")
	}
	return fmt.Errorf("remote rename is disabled in the config picker")
}

func (b *BrowserFS) Remove(ctx context.Context, p string, recursive bool) error {
	base := path.Base(p)
	switch base {
	case "metadata", "keys", "events", "checkpoints", "acks":
		return fmt.Errorf("refusing to delete syncsh repository path %s", base)
	}
	if !recursive {
		return b.tr.fs.Rmdir(ctx, strings.Trim(p, "/"))
	}
	return b.tr.Remove(ctx, p)
}

func (b *BrowserFS) Stat(ctx context.Context, p string) (picker.Entry, error) {
	dirs, err := b.List(ctx, b.Parent(p))
	if err != nil {
		return picker.Entry{}, err
	}
	want := path.Base(p)
	for _, e := range dirs {
		if e.Name == want {
			return e, nil
		}
	}
	if p == "" || p == "." {
		return picker.Entry{Name: "", Path: "", IsDir: true}, nil
	}
	return picker.Entry{}, os.ErrNotExist
}

func (b *BrowserFS) IsEmpty(ctx context.Context, p string) (bool, error) {
	objs, err := b.tr.ListShallow(ctx, p)
	if err != nil {
		return false, err
	}
	dirs, err := b.tr.ListDirs(ctx, p)
	if err != nil {
		return false, err
	}
	return len(objs) == 0 && len(dirs) == 0, nil
}

func (b *BrowserFS) CountChildren(ctx context.Context, p string, limit int) (int, bool, error) {
	objs, err := b.tr.ListShallow(ctx, p)
	if err != nil {
		return 0, false, err
	}
	dirs, err := b.tr.ListDirs(ctx, p)
	if err != nil {
		return 0, false, err
	}
	n := len(objs) + len(dirs)
	return n, n < limit, nil
}
