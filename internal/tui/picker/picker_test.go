package picker

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/dont-be-evil-company/remnix/internal/transport/directory"
)

func TestValidateDirName(t *testing.T) {
	if err := ValidateDirName(""); err == nil {
		t.Fatal("empty")
	}
	if err := ValidateDirName("a/b"); err == nil {
		t.Fatal("slash")
	}
	if err := ValidateDirName(".."); err == nil {
		t.Fatal("dotdot")
	}
	if err := ValidateDirName("ok"); err != nil {
		t.Fatal(err)
	}
}

func TestCanDeleteProtected(t *testing.T) {
	if err := CanDelete("/"); err == nil {
		t.Fatal("root")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := CanDelete(home); err == nil {
		t.Fatal("home")
	}
	cfg := filepath.Join(t.TempDir(), "cfg")
	data := filepath.Join(t.TempDir(), "data")
	t.Setenv("REMNIX_CONFIG_DIR", cfg)
	t.Setenv("REMNIX_DATA_DIR", data)
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CanDelete(cfg); err == nil {
		t.Fatal("config dir")
	}
	ok := filepath.Join(t.TempDir(), "safe")
	if err := os.Mkdir(ok, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CanDelete(ok); err != nil {
		t.Fatal(err)
	}
}

func TestLocalCRUD(t *testing.T) {
	root := t.TempDir()
	fs := NewLocal(root)
	ctx := context.Background()
	child := filepath.Join(root, "foo")
	if err := fs.Mkdir(ctx, child); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(root, "bar")
	if err := fs.Rename(ctx, child, renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(renamed, "x"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fs.Remove(ctx, renamed, false); err == nil {
		t.Fatal("non-empty without recursive")
	}
	if err := fs.Remove(ctx, renamed, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(renamed); !os.IsNotExist(err) {
		t.Fatal("still exists")
	}
}

func TestRemoveDoesNotFollowDirSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	fs := NewLocal(root)
	if err := fs.Remove(context.Background(), link, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "keep")); err != nil {
		t.Fatal("symlink delete followed target")
	}
}

func TestAutocomplete(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "GoogleDrive"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "Documents"), 0o700); err != nil {
		t.Fatal(err)
	}
	fs := NewLocal(root)
	done, matches, err := Complete(context.Background(), fs, filepath.Join(root, "Goo"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0] != "GoogleDrive" {
		t.Fatalf("matches %v done %q", matches, done)
	}
	if filepath.Base(filepath.Clean(done)) != "GoogleDrive" {
		t.Fatalf("done %q", done)
	}
}

func TestClassifyDest(t *testing.T) {
	ctx := context.Background()
	empty := t.TempDir()
	rep, err := ClassifyDest(ctx, empty)
	if err != nil || rep.Kind != DestEmpty {
		t.Fatalf("%+v %v", rep, err)
	}
	unrel := t.TempDir()
	if err := os.WriteFile(filepath.Join(unrel, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err = ClassifyDest(ctx, unrel)
	if err != nil || rep.Kind != DestUnrelated {
		t.Fatalf("unrelated %+v", rep)
	}
	for _, ch := range ChoicesFor(DestValidRepo) {
		if ch == ChoiceProceedAnyway {
			t.Fatal("must not offer proceed anyway over a valid repo")
		}
	}
	valid := t.TempDir()
	tr := directory.New(valid)
	if err := tr.PutAtomic(ctx, "metadata/manifest", bytes.NewReader([]byte(`{"version":1,"active_generation":"g1"}`))); err != nil {
		t.Fatal(err)
	}
	if err := tr.PutAtomic(ctx, "keys/generations/g1/manifest", bytes.NewReader([]byte(`{"generation_id":"g1","version":1}`))); err != nil {
		t.Fatal(err)
	}
	rep, err = ClassifyDest(ctx, valid)
	if err != nil || rep.Kind != DestValidRepo {
		t.Fatalf("valid %+v err=%v", rep, err)
	}
}

func TestModelMkdirSelectAndEsc(t *testing.T) {
	root := t.TempDir()
	m := New(NewLocal(root), Options{Title: "pick"})
	got, _ := m.Update(tea.KeyPressMsg{Code: 'n'})
	gm := got.(Model)
	if gm.mode != modeMkdir {
		t.Fatalf("mode %d", gm.mode)
	}
	gm.prompt.SetValue("foo")
	got, _ = gm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	gm = got.(Model)
	if _, err := os.Stat(filepath.Join(root, "foo")); err != nil {
		t.Fatal(err)
	}
	if gm.fs.Current() != filepath.Join(root, "foo") {
		t.Fatalf("should enter new folder, current %q", gm.fs.Current())
	}
	got, _ = gm.Update(tea.KeyPressMsg{Code: ' '})
	gm = got.(Model)
	if gm.Selected().Canceled || gm.Selected().Path == "" {
		t.Fatalf("select %+v", gm.Selected())
	}
	m = New(NewLocal(root), Options{})
	got, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	gm = got.(Model)
	if !gm.Selected().Canceled {
		t.Fatal("esc should cancel")
	}
}

func TestModelRefusesProtectedDelete(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	m := New(NewLocal(home), Options{})
	m.cursor = 0
	m.visible = []Entry{{Name: filepath.Base(home), Path: home, IsDir: true}}
	got, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	gm := got.(Model)
	if gm.mode == modeDeleteEmpty || gm.mode == modeDeleteRecursive {
		t.Fatal("must not enter delete mode for home")
	}
}

func TestModelRecursiveDeleteRequiresName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "foo")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := New(NewLocal(root), Options{})
	m.focusNamed("foo")
	got, _ := m.Update(tea.KeyPressMsg{Code: 'd'})
	gm := got.(Model)
	if gm.mode != modeDeleteRecursive {
		t.Fatalf("mode %d status %s", gm.mode, gm.status)
	}
	gm.prompt.SetValue("nope")
	got, _ = gm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	gm = got.(Model)
	if _, err := os.Stat(filepath.Join(dir, "x")); err != nil {
		t.Fatal("file should remain after mismatched confirm")
	}
	m = New(NewLocal(root), Options{})
	m.focusNamed("foo")
	got, _ = m.Update(tea.KeyPressMsg{Code: 'd'})
	gm = got.(Model)
	gm.prompt.SetValue("foo")
	got, _ = gm.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	gm = got.(Model)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("should be deleted after matching name")
	}
}
