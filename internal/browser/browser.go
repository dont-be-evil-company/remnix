// Package browser opens an OAuth URL in the user's browser, with a URL fallback.
package browser

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

func Open(url string) error {
	if url == "" {
		return fmt.Errorf("empty url")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
			cmd = exec.Command("wslview", url)
			if err := cmd.Start(); err == nil {
				return nil
			}
		}
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
