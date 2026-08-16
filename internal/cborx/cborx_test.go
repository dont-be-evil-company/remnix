package cborx

import "testing"

func TestRoundTripArrayPastLibraryDefault(t *testing.T) {
	// fxamacker/cbor defaults MaxArrayElements to 131072.
	n := 131072 + 1
	in := make([]int, n)
	for i := range in {
		in[i] = i
	}
	raw, err := Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out []int
	if err := Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != n || out[0] != 0 || out[n-1] != n-1 {
		t.Fatalf("len=%d first=%d last=%d", len(out), out[0], out[n-1])
	}
}
