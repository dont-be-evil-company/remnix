// Package rclone implements a syncsh Transport over embedded rclone backends.
//
// Spike findings (rclone v1.73.4):
//   - Direct Go APIs (fs.NewFs, fs.Fs, fs.Object) work without librclone.
//   - Config is process-global; syncsh must SetConfigPath to its own rclone.conf
//     and serialize mutations with mu.
//   - Selected backend blank-imports register remotes; no external binary.
//   - CGO is not required for the featured backends (FUSE/mount is not imported).
package rclone

import (
	_ "github.com/rclone/rclone/backend/azurefiles"
	_ "github.com/rclone/rclone/backend/drive"
	_ "github.com/rclone/rclone/backend/dropbox"
	_ "github.com/rclone/rclone/backend/googlecloudstorage"
	_ "github.com/rclone/rclone/backend/iclouddrive"
	_ "github.com/rclone/rclone/backend/local"
	_ "github.com/rclone/rclone/backend/memory"
	_ "github.com/rclone/rclone/backend/onedrive"
	_ "github.com/rclone/rclone/backend/s3"
	_ "github.com/rclone/rclone/backend/smb"
	_ "github.com/rclone/rclone/backend/webdav"
)
