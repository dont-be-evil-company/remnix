package app

import (
	"testing"
)

func TestAcquireLockExclusive(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SYNCSH_DATA_DIR", dir)

	l1, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	defer l1.Release()

	if _, err := AcquireLock(); err == nil {
		t.Fatal("expected second lock to fail")
	}
	if err := l1.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := AcquireLock()
	if err != nil {
		t.Fatal(err)
	}
	l2.Release()
}
