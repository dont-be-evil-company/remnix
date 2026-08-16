package transport

import (
	"errors"
	"io/fs"
	"testing"
)

func TestMapFSError(t *testing.T) {
	if MapFSError(nil) != nil {
		t.Fatal("nil")
	}
	if !errors.Is(MapFSError(fs.ErrNotExist), ErrRemoteNotFound) {
		t.Fatal("not exist")
	}
	if !errors.Is(MapFSError(fs.ErrPermission), ErrPermissionDenied) {
		t.Fatal("permission")
	}
	if !errors.Is(MapFSError(ErrAuthRequired), ErrAuthRequired) {
		t.Fatal("passthrough")
	}
}

func TestClassifyHealth(t *testing.T) {
	if ClassifyHealth(nil).State != HealthOK {
		t.Fatal("ok")
	}
	if ClassifyHealth(ErrAuthRequired).State != HealthAuthRequired {
		t.Fatal("auth")
	}
	if ClassifyHealth(ErrOffline).State != HealthOffline {
		t.Fatal("offline")
	}
}
