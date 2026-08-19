package picker

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/mistweaverco/syncsh/internal/config"
)

func ValidateDirName(name string) error {
	if name == "" {
		return fmt.Errorf("name is empty")
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("name contains NUL")
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("name must not contain a path separator")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid name %q", name)
	}
	if runtime.GOOS == "windows" {
		invalid := `<>:"|?*`
		if strings.ContainsAny(name, invalid) {
			return fmt.Errorf("invalid characters in name")
		}
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("name is not valid UTF-8")
	}
	return nil
}

func IsFSRoot(path string) bool {
	cleaned := filepath.Clean(path)
	sep := string(filepath.Separator)
	if cleaned == sep {
		return true
	}
	vol := filepath.VolumeName(cleaned)
	if vol != "" && (cleaned == vol || cleaned == vol+sep) {
		return true
	}
	return false
}

func ProtectedPaths() []string {
	var out []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		out = append(out, filepath.Clean(home))
	}
	out = append(out, filepath.Clean(config.ConfigDir()), filepath.Clean(config.DataDir()))
	return out
}

func IsProtected(path string) bool {
	cleaned, err := filepath.Abs(path)
	if err != nil {
		cleaned = filepath.Clean(path)
	}
	if IsFSRoot(cleaned) {
		return true
	}
	for _, p := range ProtectedPaths() {
		if cleaned == p {
			return true
		}
	}
	return false
}

func CanDelete(path string) error {
	cleaned, err := filepath.Abs(path)
	if err != nil {
		cleaned = filepath.Clean(path)
	}
	canon, err := filepath.EvalSymlinks(filepath.Dir(cleaned))
	if err == nil {
		joined := filepath.Join(canon, filepath.Base(cleaned))
		if IsProtected(joined) || IsFSRoot(joined) {
			return fmt.Errorf("refusing to delete protected path %s", joined)
		}
	}
	if IsProtected(cleaned) {
		return fmt.Errorf("refusing to delete protected path %s", cleaned)
	}
	return nil
}
