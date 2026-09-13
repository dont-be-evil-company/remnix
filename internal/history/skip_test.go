package history

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/db"
)

func TestShouldSkipRecoveryKey(t *testing.T) {
	emptyIgnoreDir(t)
	if !ShouldSkip("REMNIX_RECOVERY_KEY='remnix1abc' remnix unlock") {
		t.Fatal("recovery")
	}
	if !ShouldSkip("AWS_SECRET_ACCESS_KEY=x aws s3 ls") {
		t.Fatal("aws")
	}
	if ShouldSkip("echo hi") {
		t.Fatal("plain command")
	}
}

func TestShouldSkipExactIgnore(t *testing.T) {
	dir := emptyIgnoreDir(t)
	writeIgnore(t, dir, "ignore-commands.txt", "ls\n\nll\n  exit  \n")
	if !ShouldSkip("ls") {
		t.Fatal("ls")
	}
	if ShouldSkip("ls -la") {
		t.Fatal("ls -la is not a full match")
	}
	if !ShouldSkip("ll") {
		t.Fatal("ll")
	}
	if !ShouldSkip("exit") {
		t.Fatal("trimmed exit")
	}
}

func TestShouldSkipRegexIgnore(t *testing.T) {
	dir := emptyIgnoreDir(t)
	writeIgnore(t, dir, "ignore-commands.regex", "^sudo \n")
	if !ShouldSkip("sudo apt update") {
		t.Fatal("sudo")
	}
	if ShouldSkip("apt update") {
		t.Fatal("unrelated")
	}
}

func TestShouldSkipInvalidRegex(t *testing.T) {
	dir := emptyIgnoreDir(t)
	writeIgnore(t, dir, "ignore-commands.regex", "(\n^echo \n")
	if !ShouldSkip("echo hi") {
		t.Fatal("valid regex still applies")
	}
	if ShouldSkip("ls") {
		t.Fatal("invalid regex must not skip everything")
	}
}

func TestShouldSkipMissingIgnoreFiles(t *testing.T) {
	emptyIgnoreDir(t)
	if ShouldSkip("ls") {
		t.Fatal("missing files")
	}
}

func TestStartCommandIgnoreDoesNotInsert(t *testing.T) {
	dir := emptyIgnoreDir(t)
	writeIgnore(t, dir, "ignore-commands.txt", "ls\n")

	d, err := db.OpenAndMigrate(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	store := NewStore(d)
	existing := Entry{
		ID:        "old-ls",
		Command:   "ls",
		StartTS:   time.UnixMilli(1).UTC(),
		Cwd:       "/tmp",
		DeviceID:  "dev",
		CreatedAt: time.UnixMilli(1).UTC(),
	}
	if ok, err := store.Insert(existing); err != nil || !ok {
		t.Fatalf("seed: ok=%v err=%v", ok, err)
	}

	svc := NewService(store, NewCache(), d.SQL, "dev", nil, nil)
	id, err := svc.StartCommand("ls", "/tmp", "sess", "zsh")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("skipped start still returns an id")
	}
	if _, found, err := store.Get(id); err != nil || found {
		t.Fatalf("ignored command inserted: found=%v err=%v", found, err)
	}
	got, err := store.ListByCommand("ls")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "old-ls" {
		t.Fatalf("existing row affected: %+v", got)
	}

	id, err = svc.StartCommand("echo hi", "/tmp", "sess", "zsh")
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Get(id); err != nil || !found {
		t.Fatalf("non-ignored command missing: found=%v err=%v", found, err)
	}
}

func emptyIgnoreDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("REMNIX_CONFIG_DIR", dir)
	resetUserIgnore()
	return dir
}

func writeIgnore(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	resetUserIgnore()
}

func resetUserIgnore() {
	userIgnore.mu.Lock()
	defer userIgnore.mu.Unlock()
	userIgnore.txtPath = ""
	userIgnore.rePath = ""
	userIgnore.txtMtime = time.Time{}
	userIgnore.reMtime = time.Time{}
	userIgnore.txtOK = false
	userIgnore.reOK = false
	userIgnore.rules = ignoreRules{}
}
