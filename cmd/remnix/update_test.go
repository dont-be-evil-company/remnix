package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1, v2   string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.0", "1.0.1", -1},
		{"1.0", "1.0.0", 0},
	}
	for _, tt := range tests {
		if got := compareVersions(tt.v1, tt.v2); got != tt.expected {
			t.Fatalf("compareVersions(%q, %q) = %d, want %d", tt.v1, tt.v2, got, tt.expected)
		}
	}
}

func TestDetectPlatform(t *testing.T) {
	platform := detectPlatform()
	if !strings.Contains(platform, "-") {
		t.Fatalf("platform should contain dash: %s", platform)
	}
}

func TestUpdateHelp(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetArgs([]string{"update", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCopyFile(t *testing.T) {
	srcFile, err := createTempFile("test content")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(srcFile)

	dstFile := srcFile + ".copy"
	defer os.Remove(dstFile)

	if err := copyFile(srcFile, dstFile); err != nil {
		t.Fatal(err)
	}

	srcContent, err := os.ReadFile(srcFile)
	if err != nil {
		t.Fatal(err)
	}
	dstContent, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(srcContent) != string(dstContent) {
		t.Fatalf("copied content mismatch")
	}
}

func TestReplaceBinaryWindowsStagesAndStartsHelper(t *testing.T) {
	currentPath := filepath.Join(t.TempDir(), "remnix.exe")
	newBinaryPath, err := createTempFile("new binary content")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(newBinaryPath)

	var capturedName string
	var capturedArgs []string

	originalExec := newExecCommand
	newExecCommand = func(name string, args ...string) *exec.Cmd {
		capturedName = name
		capturedArgs = args
		return exec.Command("sh", "-c", "true")
	}
	defer func() { newExecCommand = originalExec }()

	if err := replaceBinaryWindows(currentPath, newBinaryPath); err != nil {
		t.Fatal(err)
	}

	stagedPath := currentPath + ".new"
	stagedContent, err := os.ReadFile(stagedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stagedContent) != "new binary content" {
		t.Fatalf("unexpected staged content: %q", stagedContent)
	}

	if capturedName != "powershell.exe" {
		t.Fatalf("expected powershell.exe, got %q", capturedName)
	}
	if len(capturedArgs) < 5 {
		t.Fatalf("unexpected args: %v", capturedArgs)
	}

	_ = os.Remove(stagedPath)
	_ = os.Remove(capturedArgs[len(capturedArgs)-1])
}

func TestEscapePowerShellSingleQuotedPath(t *testing.T) {
	input := `C:\Users\O'Brien\bin\remnix.exe`
	escaped := escapePowerShellSingleQuotedPath(input)
	want := `C:\Users\O''Brien\bin\remnix.exe`
	if escaped != want {
		t.Fatalf("got %q, want %q", escaped, want)
	}
}

func createTempFile(content string) (string, error) {
	tmpFile, err := os.CreateTemp("", "remnix-test-*")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()
	if _, err := tmpFile.WriteString(content); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}
	return tmpFile.Name(), nil
}
