package rclone

import (
	"context"
	"path/filepath"
	"testing"
)

func TestConfigSessionLocal(t *testing.T) {
	dir := t.TempDir()
	if err := Init(filepath.Join(dir, "rclone.conf")); err != nil {
		t.Fatal(err)
	}
	s := NewConfigSession("local", "synctestlocal")
	defer s.Abort()
	q, err := s.Step(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	// Local backend may finish immediately or ask questions; either is fine.
	for i := 0; i < 20 && q != nil && !s.Finished; i++ {
		ans := q.Default
		if ans == "" && len(q.Choices) > 0 {
			ans = q.Choices[0].Value
		}
		q, err = s.Step(context.Background(), ans)
		if err != nil {
			t.Fatal(err)
		}
	}
	found := false
	for _, n := range ListSections() {
		if n == s.Name {
			found = true
		}
	}
	if !found && !s.Finished {
		t.Fatalf("expected section %s, finished=%v q=%v", s.Name, s.Finished, q)
	}
}

func TestBackendTypesIncludeLocal(t *testing.T) {
	found := false
	for _, n := range BackendTypes() {
		if n == "local" {
			found = true
		}
	}
	if !found {
		t.Fatalf("local backend not registered: %v", BackendTypes())
	}
}
