//go:build linux

package daemon

import (
	"strings"
	"testing"
)

func TestUnitContents(t *testing.T) {
	s := unitContents("/opt/syncsh")
	if !strings.Contains(s, "ExecStart=/opt/syncsh daemon") {
		t.Fatalf("missing ExecStart:\n%s", s)
	}
	if !strings.Contains(s, "WantedBy=default.target") {
		t.Fatalf("missing WantedBy:\n%s", s)
	}
}
