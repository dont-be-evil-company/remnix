package redact

import (
	"log/slog"
	"strings"
	"testing"
)

func TestString(t *testing.T) {
	in := `token=abc123 password: hunter2 REMNIX_RECOVERY_KEY=remnix1abcde`
	got := String(in)
	if strings.Contains(got, "abc123") || strings.Contains(got, "hunter2") {
		t.Fatalf("leaked: %s", got)
	}
}

func TestIsSecretKey(t *testing.T) {
	if !IsSecretKey("secret_access_key") || IsSecretKey("bucket") || IsSecretKey("monkey") {
		t.Fatal("keys")
	}
	if !IsSecretKey("client_secret") || !IsSecretKey("password") {
		t.Fatal("secret names")
	}
}

func TestReplaceAttr(t *testing.T) {
	a := ReplaceAttr(nil, slog.String("err", "password=hunter2"))
	if strings.Contains(a.Value.String(), "hunter2") {
		t.Fatal(a.Value.String())
	}
}
