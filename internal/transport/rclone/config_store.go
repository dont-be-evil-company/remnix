package rclone

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rclone/rclone/fs/config"
	"github.com/unknwon/goconfig"
)

func UserConfigPath() string {
	if p := os.Getenv("RCLONE_CONFIG"); p != "" {
		return p
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "rclone", "rclone.conf")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "rclone", "rclone.conf")
}

func ListSections() []string {
	mu.Lock()
	defer mu.Unlock()
	return config.FileSections()
}

func DeleteSection(name string) {
	mu.Lock()
	defer mu.Unlock()
	config.LoadedData().DeleteSection(name)
	config.SaveConfig()
}

func RenameSection(oldName, newName string) error {
	mu.Lock()
	defer mu.Unlock()
	if oldName == newName {
		return nil
	}
	keys := config.LoadedData().GetKeyList(oldName)
	if len(keys) == 0 {
		return fmt.Errorf("remote %q not found", oldName)
	}
	config.LoadedData().DeleteSection(newName)
	for _, k := range keys {
		v, _ := config.FileGetValue(oldName, k)
		config.FileSetValue(newName, k, v)
	}
	config.LoadedData().DeleteSection(oldName)
	config.SaveConfig()
	return nil
}

func ImportUserRemote(srcName, dstName string) error {
	src := UserConfigPath()
	if src == "" {
		return fmt.Errorf("could not locate user rclone config")
	}
	gc, err := goconfig.LoadConfigFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	keys := gc.GetKeyList(srcName)
	if len(keys) == 0 {
		return fmt.Errorf("remote %q not found in %s", srcName, src)
	}
	mu.Lock()
	defer mu.Unlock()
	config.LoadedData().DeleteSection(dstName)
	for _, k := range keys {
		v, err := gc.GetValue(srcName, k)
		if err != nil {
			continue
		}
		config.FileSetValue(dstName, k, v)
	}
	_ = hardenRemoteLocked(dstName)
	config.SaveConfig()
	return nil
}

func ListUserRemotes() ([]string, error) {
	src := UserConfigPath()
	if src == "" {
		return nil, nil
	}
	gc, err := goconfig.LoadConfigFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, s := range gc.GetSectionList() {
		if strings.EqualFold(s, "DEFAULT") {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// HardenRemote sets safe defaults on cloud backends (skip Google Docs, etc.).
func HardenRemote(name string) {
	if name == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	if hardenRemoteLocked(name) {
		config.SaveConfig()
	}
}

func hardenRemoteLocked(name string) bool {
	typ, _ := config.FileGetValue(name, "type")
	if typ != "drive" {
		return false
	}
	changed := false
	if v, _ := config.FileGetValue(name, "skip_gdocs"); v != "true" {
		config.FileSetValue(name, "skip_gdocs", "true")
		changed = true
	}
	if v, _ := config.FileGetValue(name, "skip_dangling_shortcuts"); v != "true" {
		config.FileSetValue(name, "skip_dangling_shortcuts", "true")
		changed = true
	}
	return changed
}
