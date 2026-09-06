package daemon

import (
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/mistweaverco/syncsh/internal/config"
	"github.com/mistweaverco/syncsh/internal/redact"
)

type Status struct {
	OK         bool   `json:"ok"`
	At         int64  `json:"at"`
	HumanAt    string `json:"human_at"`
	Error      string `json:"error,omitempty"`
	Class      string `json:"class,omitempty"`
	Hint       string `json:"hint,omitempty"`
	GCDeleted  int    `json:"gc_deleted,omitempty"`
	GCEligible bool   `json:"gc_eligible,omitempty"`
	SyncMs     int64  `json:"sync_ms,omitempty"`
	GCMs       int64  `json:"gc_ms,omitempty"`
	PID        int    `json:"pid,omitempty"`
	UptimeSec  int64  `json:"uptime_sec,omitempty"`
}

func FormatElapsed(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return (time.Duration(ms) * time.Millisecond).String()
}

func (st Status) FormatTiming() string {
	var b strings.Builder
	if s := FormatElapsed(st.SyncMs); s != "" {
		b.WriteString(" sync=")
		b.WriteString(s)
	}
	if s := FormatElapsed(st.GCMs); s != "" {
		b.WriteString(" gc=")
		b.WriteString(s)
	}
	return b.String()
}

func RecordOK() {
	now := time.Now()
	writeStatus(Status{OK: true, At: now.Unix(), HumanAt: now.Format(time.RFC3339), Class: "manual"})
}

func writeStatus(st Status) {
	st.Error = redact.String(st.Error)
	st.Hint = redact.String(st.Hint)
	_ = os.MkdirAll(config.DataDir(), 0o700)
	b, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.WriteFile(config.DaemonStatusPath(), b, 0o600)
}

func ReadStatus() (Status, error) {
	b, err := os.ReadFile(config.DaemonStatusPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Status{}, nil
		}
		return Status{}, err
	}
	var st Status
	if err := json.Unmarshal(b, &st); err != nil {
		return Status{}, err
	}
	return st, nil
}
