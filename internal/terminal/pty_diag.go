package terminal

import (
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// REMNIX_PTY_DIAG enables opt-in PTY latency/integrity counters.
// Set to "1" or "true" for summaries, "trace" for rare extra logs.
// Disabled by default; the hot path stays atomic adds of integers.
const envPTYDiag = "REMNIX_PTY_DIAG"

type ptyDiagState struct {
	on    atomic.Bool
	trace atomic.Bool

	framesRecv       atomic.Uint64
	decoderRelease   atomic.Uint64
	ptyWriteStarts   atomic.Uint64
	ptyWriteComplete atomic.Uint64
	ptyShortWrites   atomic.Uint64
	ptyEAGAIN        atomic.Uint64
	ptyEINTR         atomic.Uint64
	ptyWriteErrors   atomic.Uint64
	inputQueued      atomic.Uint64
	inputMaxDepth    atomic.Uint64
	inputSaturated   atomic.Uint64
	parseSaturations atomic.Uint64
	writeNsSum       atomic.Uint64
	writeNsMax       atomic.Uint64
	writeNsCount     atomic.Uint64
}

var ptyDiag ptyDiagState

func init() {
	configurePTYDiag(os.Getenv(envPTYDiag))
}

func configurePTYDiag(v string) {
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "", "0", "false", "off":
		ptyDiag.on.Store(false)
		ptyDiag.trace.Store(false)
	case "trace":
		ptyDiag.on.Store(true)
		ptyDiag.trace.Store(true)
	default:
		ptyDiag.on.Store(true)
		ptyDiag.trace.Store(false)
	}
}

func ptyDiagEnabled() bool { return ptyDiag.on.Load() }

func ptyDiagTrace() bool { return ptyDiag.trace.Load() }

func diagAddMax(dst *atomic.Uint64, v uint64) {
	for {
		old := dst.Load()
		if v <= old {
			return
		}
		if dst.CompareAndSwap(old, v) {
			return
		}
	}
}

func diagObserveWrite(d time.Duration) {
	if !ptyDiagEnabled() || d < 0 {
		return
	}
	ns := uint64(d.Nanoseconds())
	ptyDiag.writeNsSum.Add(ns)
	ptyDiag.writeNsCount.Add(1)
	diagAddMax(&ptyDiag.writeNsMax, ns)
}
