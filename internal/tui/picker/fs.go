// Package picker is a local/remote filesystem browser for the config wizard.
package picker

import (
	"context"
	"os"
)

type Entry struct {
	Name          string
	Path          string
	IsDir         bool
	IsSymlink     bool
	Inaccessible  bool
	DisplaySuffix string
}

type Capabilities struct {
	Mkdir     bool
	Rename    bool
	Remove    bool
	Expensive bool
}

type BrowserFS interface {
	Current() string
	SetCurrent(path string)
	List(ctx context.Context, path string) ([]Entry, error)
	Mkdir(ctx context.Context, path string) error
	Rename(ctx context.Context, oldPath, newPath string) error
	Remove(ctx context.Context, path string, recursive bool) error
	Join(elem ...string) string
	Parent(path string) string
	Clean(path string) string
	Abs(path string) (string, error)
	Stat(ctx context.Context, path string) (Entry, error)
	IsEmpty(ctx context.Context, path string) (bool, error)
	CountChildren(ctx context.Context, path string, limit int) (n int, exact bool, err error)
	Capabilities() Capabilities
}

func fileModeEntry(path, name string, info os.FileInfo, lerr error) Entry {
	e := Entry{Name: name, Path: path}
	if lerr != nil {
		e.Inaccessible = true
		return e
	}
	e.IsSymlink = info.Mode()&os.ModeSymlink != 0
	if e.IsSymlink {
		e.DisplaySuffix = "@"
		if t, err := os.Stat(path); err == nil {
			e.IsDir = t.IsDir()
		}
	} else {
		e.IsDir = info.IsDir()
	}
	if e.IsDir {
		e.DisplaySuffix = "/"
		if e.IsSymlink {
			e.DisplaySuffix = "@/"
		}
	}
	return e
}
