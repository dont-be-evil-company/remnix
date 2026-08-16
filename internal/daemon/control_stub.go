//go:build !linux && !darwin

package daemon

import "fmt"

func restartService() error {
	return fmt.Errorf("service restart is not supported on this OS")
}
