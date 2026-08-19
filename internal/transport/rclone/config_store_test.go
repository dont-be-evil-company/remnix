package rclone

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rclone/rclone/fs/config"
)

func TestImportUserRemote(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "user-rclone.conf")
	if err := os.WriteFile(user, []byte("[mydrive]\ntype = local\nnounc =\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("RCLONE_CONFIG", user)
	if err := Init(filepath.Join(dir, "syncsh-rclone.conf")); err != nil {
		t.Fatal(err)
	}
	names, err := ListUserRemotes()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range names {
		if n == "mydrive" {
			found = true
		}
	}
	if !found {
		t.Fatalf("user remotes: %v", names)
	}
	if err := ImportUserRemote("mydrive", "copied"); err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, n := range ListSections() {
		if n == "copied" {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("imported section missing: %v", ListSections())
	}
}

func TestHardenDriveRemote(t *testing.T) {
	dir := t.TempDir()
	if err := Init(filepath.Join(dir, "syncsh-rclone.conf")); err != nil {
		t.Fatal(err)
	}
	Lock()
	config.FileSetValue("gdrive", "type", "drive")
	config.SaveConfig()
	Unlock()
	HardenRemote("gdrive")
	Lock()
	defer Unlock()
	if v, _ := config.FileGetValue("gdrive", "skip_gdocs"); v != "true" {
		t.Fatalf("skip_gdocs=%q", v)
	}
	if v, _ := config.FileGetValue("gdrive", "skip_dangling_shortcuts"); v != "true" {
		t.Fatalf("skip_dangling_shortcuts=%q", v)
	}
}

func TestFirstURL(t *testing.T) {
	if got := firstURL("Visit https://example.com/oauth?x=1 then continue"); got != "https://example.com/oauth?x=1" {
		t.Fatalf("got %q", got)
	}
}
