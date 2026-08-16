//go:build linux

package daemon

import (
	"strings"
	"testing"
)

func TestUnitContents(t *testing.T) {
	s := unitContents("/opt/remnix")
	if !strings.Contains(s, "ExecStart=/opt/remnix daemon") {
		t.Fatalf("missing ExecStart:\n%s", s)
	}
	if !strings.Contains(s, "WantedBy=default.target") {
		t.Fatalf("missing WantedBy:\n%s", s)
	}
	if !strings.Contains(s, "TimeoutStopSec=600") {
		t.Fatalf("missing TimeoutStopSec:\n%s", s)
	}
	if !strings.Contains(s, "ExecReload=/bin/kill -HUP $MAINPID") {
		t.Fatalf("missing ExecReload:\n%s", s)
	}
}
