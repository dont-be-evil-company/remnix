package syncer

import "github.com/mistweaverco/syncsh/internal/sync/event"

func newTombstone(deviceID string, seq int64, historyID, origin string, originSeq int64) (event.Event, error) {
	return event.NewHistoryTombstoned(deviceID, seq, event.HistoryTombstoned{
		HistoryID:      historyID,
		OriginDeviceID: origin,
		OriginSeq:      originSeq,
	})
}
