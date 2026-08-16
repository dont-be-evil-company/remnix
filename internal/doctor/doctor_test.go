package doctor

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dont-be-evil-company/remnix/internal/app"
	"github.com/dont-be-evil-company/remnix/internal/crypto/keyring"
	"github.com/dont-be-evil-company/remnix/internal/setup"
)

func TestMain(m *testing.M) {
	keyring.Use(keyring.NewMemory())
	os.Exit(m.Run())
}

func TestDoctorReportsFIDO2HID(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REMNIX_CONFIG_DIR", filepath.Join(root, "cfg"))
	t.Setenv("REMNIX_DATA_DIR", filepath.Join(root, "data"))
	a, err := app.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := setup.RunNonInteractive(context.Background(), a, filepath.Join(root, "remote"), "test", nil, nil); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := Run(context.Background(), a, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "fido2 hid") {
		t.Fatalf("expected fido2 hid diagnostics, got:\n%s", out)
	}
	if !strings.Contains(out, "piv:") {
		t.Fatalf("expected piv diagnostics, got:\n%s", out)
	}
	if !strings.Contains(out, "keyring:") {
		t.Fatalf("expected keyring diagnostics, got:\n%s", out)
	}
	if !strings.Contains(out, "daemon") {
		t.Fatalf("expected daemon diagnostics, got:\n%s", out)
	}
}
