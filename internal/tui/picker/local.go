package picker

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type LocalFS struct {
	cwd string
}

func NewLocal(start string) *LocalFS {
	if start == "" {
		start, _ = os.UserHomeDir()
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		abs = start
	}
	return &LocalFS{cwd: abs}
}

func (l *LocalFS) Current() string { return l.cwd }

func (l *LocalFS) SetCurrent(path string) {
	l.cwd = l.Clean(path)
}

func (l *LocalFS) Clean(path string) string {
	return filepath.Clean(path)
}

func (l *LocalFS) Join(elem ...string) string {
	return filepath.Join(elem...)
}

func (l *LocalFS) Parent(path string) string {
	p := filepath.Dir(filepath.Clean(path))
	if p == path {
		return path
	}
	return p
}

func (l *LocalFS) Abs(path string) (string, error) {
	return filepath.Abs(path)
}

func (l *LocalFS) Capabilities() Capabilities {
	return Capabilities{Mkdir: true, Rename: true, Remove: true}
}

func (l *LocalFS) Stat(_ context.Context, path string) (Entry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Entry{}, err
	}
	return fileModeEntry(path, filepath.Base(path), info, nil), nil
}

func (l *LocalFS) List(_ context.Context, path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names, err := f.Readdirnames(0)
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	out := make([]Entry, 0, len(names))
	for _, name := range names {
		p := filepath.Join(path, name)
		info, lerr := os.Lstat(p)
		e := fileModeEntry(p, name, info, lerr)
		if lerr == nil && !e.IsDir && !e.IsSymlink {
			continue
		}
		if lerr == nil && !e.IsDir {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func (l *LocalFS) Mkdir(_ context.Context, path string) error {
	return os.Mkdir(path, 0o700)
}

func (l *LocalFS) Rename(_ context.Context, oldPath, newPath string) error {
	if _, err := os.Lstat(newPath); err == nil {
		return fmt.Errorf("already exists: %s", filepath.Base(newPath))
	}
	return os.Rename(oldPath, newPath)
}

func (l *LocalFS) Remove(_ context.Context, path string, recursive bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(path)
	}
	if !info.IsDir() {
		return os.Remove(path)
	}
	if !recursive {
		return os.Remove(path)
	}
	return removeDirNoFollow(path)
}

func removeDirNoFollow(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(path, e.Name())
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			if err := os.Remove(p); err != nil {
				return err
			}
			continue
		}
		if err := removeDirNoFollow(p); err != nil {
			return err
		}
	}
	return os.Remove(path)
}

func (l *LocalFS) IsEmpty(_ context.Context, path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return len(entries) == 0, nil
}

func (l *LocalFS) CountChildren(_ context.Context, path string, limit int) (int, bool, error) {
	n := 0
	err := filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsPermission(err) {
				return nil
			}
			return err
		}
		if p == path {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			n++
			if n >= limit {
				return fs.SkipAll
			}
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		n++
		if n >= limit {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return n, n < limit, err
	}
	return n, true, nil
}

func hiddenName(name string) bool {
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}
