package rclone

import (
	"os"
	"path/filepath"
	"sync"

	configpkg "github.com/dont-be-evil-company/remnix/internal/config"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/configfile"
)

var (
	mu       sync.Mutex
	initOnce sync.Once
	initErr  error
)

func Init(confPath string) error {
	initOnce.Do(func() {
		_ = configpkg.MigrateRcloneConfig()
		if confPath == "" {
			confPath = configpkg.RcloneConfigPath()
		}
		if err := os.MkdirAll(filepath.Dir(confPath), 0o700); err != nil {
			initErr = err
			return
		}
		if st, err := os.Stat(confPath); os.IsNotExist(err) {
			if err := os.WriteFile(confPath, []byte(""), 0o600); err != nil {
				initErr = err
				return
			}
		} else if err != nil {
			initErr = err
			return
		} else if st.Mode().Perm()&0o077 != 0 {
			_ = os.Chmod(confPath, 0o600)
		}
		if err := config.SetConfigPath(confPath); err != nil {
			initErr = err
			return
		}
		configfile.Install()
	})
	return initErr
}

func Lock()   { mu.Lock() }
func Unlock() { mu.Unlock() }

func Version() string {
	return fs.Version
}
