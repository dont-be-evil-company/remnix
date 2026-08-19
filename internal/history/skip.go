package history

import "strings"

func ShouldSkip(command string) bool {
	u := strings.ToUpper(command)
	for _, p := range []string{
		"SYNCSH_RECOVERY_KEY=",
		"RCLONE_CONFIG_PASS=",
		"AWS_SECRET_ACCESS_KEY=",
		"AZURE_STORAGE_KEY=",
	} {
		if strings.Contains(u, p) {
			return true
		}
	}
	return false
}
