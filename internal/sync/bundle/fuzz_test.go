package bundle

import "testing"

func FuzzUnpack(f *testing.F) {
	f.Add([]byte{0x00})
	f.Add([]byte("not-cbor"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _, _ = Unpack(data, make([]byte, 32))
	})
}
