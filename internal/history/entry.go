package history

import "time"

type Entry struct {
	ID             string
	Command        string
	StartTS        time.Time
	EndTS          *time.Time
	DurationMs     *int64
	ExitStatus     *int
	Cwd            string
	SessionID      string
	Hostname       string
	DeviceID       string
	Shell          string
	Deleted        bool
	OriginDeviceID string
	OriginSeq      *int64
	CreatedAt      time.Time
}
