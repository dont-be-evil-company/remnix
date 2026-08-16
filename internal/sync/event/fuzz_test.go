package event

import "testing"

func FuzzDecode(f *testing.F) {
	ev := Event{Version: 1, Type: TypeHistoryCreated, DeviceID: "d", Seq: 1, TimeUnix: 1, Payload: []byte("x")}
	b, _ := Encode(ev)
	f.Add(b)
	f.Add([]byte{0, 1, 2, 3})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Decode(data)
	})
}
