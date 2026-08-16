package transport

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

var (
	ErrAuthRequired     = errors.New("authentication required")
	ErrOffline          = errors.New("remote unavailable")
	ErrPermissionDenied = errors.New("permission denied")
	ErrRemoteNotFound   = errors.New("remote path not found")
	ErrConflict         = errors.New("remote conflict")
	ErrSyncDisabled     = errors.New("synchronization is disabled")
)

func MapFSError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrAuthRequired) || errors.Is(err, ErrOffline) ||
		errors.Is(err, ErrPermissionDenied) || errors.Is(err, ErrRemoteNotFound) ||
		errors.Is(err, ErrConflict) || errors.Is(err, ErrSyncDisabled) {
		return err
	}
	if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		return fmt.Errorf("%w: %v", ErrRemoteNotFound, err)
	}
	if errors.Is(err, fs.ErrPermission) || os.IsPermission(err) {
		return fmt.Errorf("%w: %v", ErrPermissionDenied, err)
	}
	return err
}

func ClassifyHealth(err error) HealthStatus {
	if err == nil {
		return HealthStatus{State: HealthOK, Message: "ok"}
	}
	switch {
	case errors.Is(err, ErrAuthRequired):
		return HealthStatus{State: HealthAuthRequired, Message: err.Error()}
	case errors.Is(err, ErrOffline):
		return HealthStatus{State: HealthOffline, Message: err.Error()}
	case errors.Is(err, ErrPermissionDenied):
		return HealthStatus{State: HealthPermissionDenied, Message: err.Error()}
	case errors.Is(err, ErrRemoteNotFound):
		return HealthStatus{State: HealthNotFound, Message: err.Error()}
	case errors.Is(err, ErrSyncDisabled):
		return HealthStatus{State: HealthMisconfigured, Message: err.Error()}
	default:
		return HealthStatus{State: HealthUnknown, Message: err.Error()}
	}
}
