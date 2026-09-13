package transport

import (
	"context"
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

func TestDeleteIfExists(t *testing.T) {
	ctx := context.Background()
	base := &memFS{objs: map[string][]byte{"a": []byte("1")}}
	if err := DeleteIfExists(ctx, base, "a"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteIfExists(ctx, base, "missing"); err != nil {
		t.Fatal(err)
	}
	ft := &FaultTransport{Base: base, FailRemovePrefix: "gone", FailRemoveErr: ErrRemoteNotFound}
	if err := DeleteIfExists(ctx, ft, "gone/x"); err != nil {
		t.Fatal(err)
	}
	ft.FailRemoveErr = ErrPermissionDenied
	if err := DeleteIfExists(ctx, ft, "gone/y"); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("got %v", err)
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
