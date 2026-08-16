package merge

import (
	"sort"
)

type Frontier map[string]int64

type Range struct {
	DeviceID string
	Start    int64
	End      int64
}

func Clone(f Frontier) Frontier {
	out := make(Frontier, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out
}

func Advance(f Frontier, deviceID string, seq int64) {
	if seq > f[deviceID] {
		f[deviceID] = seq
	}
}

func Missing(local, remote Frontier) []Range {
	var out []Range
	for deviceID, remoteSeq := range remote {
		localSeq := local[deviceID]
		if remoteSeq > localSeq {
			out = append(out, Range{DeviceID: deviceID, Start: localSeq + 1, End: remoteSeq})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	return out
}

func Covers(f Frontier, deviceID string, seq int64) bool {
	return f[deviceID] >= seq
}

func Dominates(a, b Frontier) bool {
	for id, seq := range b {
		if a[id] < seq {
			return false
		}
	}
	return true
}

func Union(a, b Frontier) Frontier {
	out := Clone(a)
	for id, seq := range b {
		if seq > out[id] {
			out[id] = seq
		}
	}
	return out
}
