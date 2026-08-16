package piv

import (
	"errors"
	"strings"
	"testing"
)

func TestAnnotatePCSC(t *testing.T) {
	err := Annotate(errors.New("list yubikeys: connecting to pcsc: the Smart card resource manager is not running"))
	if err == nil || !strings.Contains(err.Error(), "pcscd") {
		t.Fatalf("got %v", err)
	}
}
