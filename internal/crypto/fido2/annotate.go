package fido2

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

const hidHelp = `
Could not open the FIDO2 hidraw device. Plug the key in over USB and check that
this user can access it (lsusb, then ls -l /dev/hidraw*). This is a device
permission, not a missing package - do not install pcscd for a Security Key.`

func Annotate(err error) error {
	if err == nil {
		return nil
	}
	if isHIDAccess(err) {
		return fmt.Errorf("%w%s", err, hidHelp)
	}
	return err
}

func isHIDAccess(err error) bool {
	if errors.Is(err, fs.ErrPermission) || errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "permission denied") ||
		strings.Contains(s, "hidraw") ||
		strings.Contains(s, "no such file") ||
		strings.Contains(s, "operation not permitted")
}
