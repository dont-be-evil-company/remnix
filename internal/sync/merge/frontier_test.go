package merge

import "testing"

func TestMissingRanges(t *testing.T) {
	local := Frontier{"a": 3, "b": 1}
	remote := Frontier{"a": 5, "b": 1, "c": 2}
	got := Missing(local, remote)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got[0].DeviceID != "a" || got[0].Start != 4 || got[0].End != 5 {
		t.Fatalf("a: %+v", got[0])
	}
	if got[1].DeviceID != "c" || got[1].Start != 1 || got[1].End != 2 {
		t.Fatalf("c: %+v", got[1])
	}
}

func TestDominates(t *testing.T) {
	a := Frontier{"x": 3, "y": 2}
	b := Frontier{"x": 2, "y": 2}
	if !Dominates(a, b) || Dominates(b, a) {
		t.Fatal("dominates")
	}
}
