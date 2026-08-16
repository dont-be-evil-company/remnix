package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/dont-be-evil-company/remnix/internal/version"
	"github.com/spf13/cobra"
)

const (
	githubRepo     = "dont-be-evil-company/remnix"
	binaryAssetName = "remnix"
)

var (
	currentGOOS    = runtime.GOOS
	newExecCommand = exec.Command
)

type gitHubRelease struct {
	TagName string `json:"tag_name"`
}

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update remnix to the latest version",
		Long: `Check if a newer version of remnix is available and update to it if found.

This command will:
1. Check the current version against the latest GitHub release
2. If a newer version is available, download it
3. Backup the current binary
4. Replace the current binary with the new version

The update process follows the same backup strategy as the installation scripts.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate()
		},
	}
}

func getLatestVersion() (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/" + githubRepo + "/releases/latest")
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var release gitHubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("failed to parse release data: %w", err)
	}
	return release.TagName, nil
}

func compareVersions(v1, v2 string) int {
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	maxLen := len(parts1)
	if len(parts2) > maxLen {
		maxLen = len(parts2)
	}
	for len(parts1) < maxLen {
		parts1 = append(parts1, "0")
	}
	for len(parts2) < maxLen {
		parts2 = append(parts2, "0")
	}

	for i := 0; i < maxLen; i++ {
		var num1, num2 int
		fmt.Sscanf(parts1[i], "%d", &num1)
		fmt.Sscanf(parts2[i], "%d", &num2)
		if num1 < num2 {
			return -1
		}
		if num1 > num2 {
			return 1
		}
	}
	return 0
}

func getCurrentBinaryPath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to get executable path: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		resolvedPath = execPath
	}
	return resolvedPath, nil
}

func detectPlatform() string {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	switch arch {
	case "amd64", "arm64":
		// keep as-is
	case "arm":
		arch = "armv7"
	default:
		arch = "amd64"
	}
	return fmt.Sprintf("%s-%s", osName, arch)
}

func downloadBinary(versionTag, platform string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Minute}

	fileName := fmt.Sprintf("%s-%s", binaryAssetName, platform)
	if strings.HasPrefix(platform, "windows-") {
		fileName += ".exe"
	}
	downloadURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", githubRepo, versionTag, fileName)

	tempFile, err := os.CreateTemp("", "remnix-update-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer tempFile.Close()

	resp, err := client.Get(downloadURL)
	if err != nil {
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("failed to download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("failed to download binary: HTTP %d", resp.StatusCode)
	}

	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		os.Remove(tempFile.Name())
		return "", fmt.Errorf("failed to save binary: %w", err)
	}

	if !strings.HasPrefix(platform, "windows-") {
		if err := os.Chmod(tempFile.Name(), 0o755); err != nil {
			os.Remove(tempFile.Name())
			return "", fmt.Errorf("failed to make binary executable: %w", err)
		}
	}
	return tempFile.Name(), nil
}

func backupCurrentBinary(binaryPath string) (string, error) {
	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.backup.%s", binaryPath, timestamp)
	if err := copyFile(binaryPath, backupPath); err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}
	return backupPath, nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return err
	}
	return os.Chmod(dst, sourceInfo.Mode())
}

func replaceBinary(currentPath, newBinaryPath string) error {
	if currentGOOS == "windows" {
		return replaceBinaryWindows(currentPath, newBinaryPath)
	}
	if err := os.Remove(currentPath); err != nil {
		return fmt.Errorf("failed to remove current binary: %w", err)
	}
	if err := copyFile(newBinaryPath, currentPath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}
	return nil
}

func replaceBinaryWindows(currentPath, newBinaryPath string) error {
	stagedPath := currentPath + ".new"
	if err := copyFile(newBinaryPath, stagedPath); err != nil {
		return fmt.Errorf("failed to stage new binary: %w", err)
	}

	scriptFile, err := os.CreateTemp("", "remnix-update-*.ps1")
	if err != nil {
		_ = os.Remove(stagedPath)
		return fmt.Errorf("failed to create updater script: %w", err)
	}
	defer scriptFile.Close()

	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$current = '%s'
$staged = '%s'
$maxAttempts = 120

for ($i = 0; $i -lt $maxAttempts; $i++) {
  try {
    Copy-Item -Path $staged -Destination $current -Force
    Remove-Item -Path $staged -Force -ErrorAction SilentlyContinue
    Remove-Item -Path $PSCommandPath -Force -ErrorAction SilentlyContinue
    exit 0
  } catch {
    Start-Sleep -Milliseconds 500
  }
}

exit 1
`, escapePowerShellSingleQuotedPath(currentPath), escapePowerShellSingleQuotedPath(stagedPath))

	if _, err := scriptFile.WriteString(script); err != nil {
		_ = os.Remove(stagedPath)
		_ = os.Remove(scriptFile.Name())
		return fmt.Errorf("failed to write updater script: %w", err)
	}

	cmd := newExecCommand("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptFile.Name())
	if err := cmd.Start(); err != nil {
		_ = os.Remove(stagedPath)
		_ = os.Remove(scriptFile.Name())
		return fmt.Errorf("failed to start updater helper: %w", err)
	}
	return nil
}

func escapePowerShellSingleQuotedPath(path string) string {
	return strings.ReplaceAll(path, "'", "''")
}

func runUpdate() error {
	currentVersion := version.Version
	slog.Debug("current version", "version", currentVersion)

	latestVersion, err := getLatestVersion()
	if err != nil {
		return fmt.Errorf("failed to get latest version: %w", err)
	}
	slog.Debug("latest version", "version", latestVersion)

	if compareVersions(currentVersion, latestVersion) >= 0 {
		fmt.Printf("remnix is already up to date (version %s)\n", currentVersion)
		return nil
	}

	fmt.Printf("New version available: %s (current: %s)\n", latestVersion, currentVersion)

	currentPath, err := getCurrentBinaryPath()
	if err != nil {
		return fmt.Errorf("failed to get current binary path: %w", err)
	}
	slog.Debug("current binary path", "path", currentPath)

	platform := detectPlatform()
	slog.Debug("detected platform", "platform", platform)

	fmt.Printf("Downloading remnix %s for %s...\n", latestVersion, platform)
	newBinaryPath, err := downloadBinary(latestVersion, platform)
	if err != nil {
		return fmt.Errorf("failed to download new version: %w", err)
	}
	defer os.Remove(newBinaryPath)

	fmt.Printf("Creating backup of current binary...\n")
	backupPath, err := backupCurrentBinary(currentPath)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	fmt.Printf("Backup created: %s\n", backupPath)

	fmt.Printf("Installing new version...\n")
	if err := replaceBinary(currentPath, newBinaryPath); err != nil {
		return fmt.Errorf("failed to replace binary: %w", err)
	}

	if currentGOOS == "windows" {
		fmt.Printf("Update scheduled from %s to %s\n", currentVersion, latestVersion)
		fmt.Println("Please wait a few seconds, then run: remnix version")
	} else {
		fmt.Printf("Successfully updated remnix from %s to %s\n", currentVersion, latestVersion)
	}
	fmt.Printf("Backup saved as: %s\n", backupPath)
	return nil
}
