// Package redact strips secrets from logs, doctor output, daemon status, and callback tails.
package redact

import (
	"log/slog"
	"regexp"
	"strings"
)

var secretNames = []string{
	"password", "pass", "token", "refresh_token", "access_token",
	"client_secret", "secret_access_key", "sas_url", "connection_string",
	"service_account_credentials", "cookie", "bearer_token",
}

var (
	kv   = regexp.MustCompile(`(?i)(password|pass|token|refresh_token|access_token|client_secret|secret_access_key|sas_url|connection_string|service_account_credentials|cookie|bearer_token|REMNIX_RECOVERY_KEY|RCLONE_CONFIG_PASS|AWS_SECRET_ACCESS_KEY)\s*[=:]\s*(\S+)`)
	bech = regexp.MustCompile(`remnix1[0-9a-z]+`)
)

func String(s string) string {
	s = kv.ReplaceAllString(s, "$1=***")
	s = bech.ReplaceAllString(s, "remnix1***")
	return s
}

func IsSecretKey(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "-", "_")
	for _, k := range secretNames {
		if n == k || strings.HasSuffix(n, "_"+k) || strings.HasPrefix(n, k+"_") {
			return true
		}
	}
	if n == "key" || strings.HasSuffix(n, "_key") {
		return true
	}
	return false
}

func ReplaceAttr(_ []string, a slog.Attr) slog.Attr {
	if IsSecretKey(a.Key) && a.Value.Kind() == slog.KindString {
		a.Value = slog.StringValue("***")
		return a
	}
	switch a.Value.Kind() {
	case slog.KindString:
		a.Value = slog.StringValue(String(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok && err != nil {
			a.Value = slog.StringValue(String(err.Error()))
		}
	}
	return a
}
