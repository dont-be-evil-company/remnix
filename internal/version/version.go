package version

import (
	rclonetr "github.com/mistweaverco/syncsh/internal/transport/rclone"
)

const Version = "0.1.0"

func Verbose() string {
	return Version + "\nrclone engine " + rclonetr.Version() + " (embedded)\n"
}
