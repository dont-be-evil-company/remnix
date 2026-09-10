package app

import (
	"strings"
	"testing"
)

func TestAcquireLockExclusive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REMNIX_DATA_DIR", dir)

	l1, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l1.Release() }()

	if _, err := AcquireLock(); err == nil {
		t.Fatal("expected second lock to fail")
	} else if !strings.Contains(err.Error(), "another remnix process holds") {
		t.Fatalf("err=%v", err)
	}
	if err := l1.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	if err := l2.Release(); err != nil {
		t.Fatal(err)
	}
}
