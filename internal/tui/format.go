package tui

import (
	"fmt"
	"time"

	"github.com/mistweaverco/syncsh/internal/history"
)

func formatDuration(ms *int64) string {
	if ms == nil || *ms < 0 {
		return ""
	}
	d := time.Duration(*ms) * time.Millisecond
	switch {
	case d < 10*time.Millisecond:
		return ""
	case d < time.Second:
		return fmt.Sprintf("%dms", *ms)
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Round(time.Second)/time.Second))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		h := int(d.Hours())
		if h >= 24 {
			return fmt.Sprintf("%dd", h/24)
		}
		return fmt.Sprintf("%dh", h)
	}
}

func formatRelative(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < 2*time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Local().Format("Jan 02")
	}
}

func failed(e history.Entry) bool {
	return e.ExitStatus != nil && *e.ExitStatus != 0
}
