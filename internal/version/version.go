package version

import (
	rclonetr "github.com/dont-be-evil-company/remnix/internal/transport/rclone"
)

const Version = "0.1.0"

func Verbose() string {
	return Version + "\nrclone engine " + rclonetr.Version() + " (embedded)\n"
}
