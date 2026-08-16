package cborx

import "github.com/fxamacker/cbor/v2"

// Shell history is not guaranteed to be UTF-8. Default fxamacker/cbor
// encodes Go strings as CBOR text without checking, then rejects the
// same bytes on decode. These modes keep that encode behavior and accept
// invalid UTF-8 (and byte-string-as-string) on decode.
//
// Checkpoints and event bundles are one CBOR array of rows. The library
// default MaxArrayElements is 131072, which is below a large local
// history (the 1M-command scale test, and real DBs that grow past it).

const maxArrayElements = 16_777_216 // 2^24

var (
	encMode cbor.EncMode
	decMode cbor.DecMode
)

func init() {
	em, err := cbor.EncOptions{}.EncMode()
	if err != nil {
		panic(err)
	}
	dm, err := cbor.DecOptions{
		UTF8:               cbor.UTF8DecodeInvalid,
		ByteStringToString: cbor.ByteStringToStringAllowed,
		MaxArrayElements:   maxArrayElements,
	}.DecMode()
	if err != nil {
		panic(err)
	}
	encMode = em
	decMode = dm
}

func Marshal(v any) ([]byte, error) {
	return encMode.Marshal(v)
}

func Unmarshal(data []byte, v any) error {
	return decMode.Unmarshal(data, v)
}
