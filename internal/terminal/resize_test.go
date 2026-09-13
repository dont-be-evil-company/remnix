package terminal

import "testing"

func TestSizeCoalescerLatestWins(t *testing.T) {
	c := newSizeCoalescer()
	c.note(80, 24)
	c.note(1, 1)
	c.note(80, 24)
	sz, ok := c.take()
	if !ok {
		t.Fatal("expected pending size")
	}
	if sz.cols != 80 || sz.rows != 24 {
		t.Fatalf("got %dx%d want 80x24", sz.cols, sz.rows)
	}
	if _, ok := c.take(); ok {
		t.Fatal("second take should be empty")
	}
}

func TestSizeCoalescerStableSmall(t *testing.T) {
	c := newSizeCoalescer()
	c.note(1, 1)
	sz, ok := c.take()
	if !ok || sz.cols != 1 || sz.rows != 1 {
		t.Fatalf("stable 1x1 must apply, ok=%v %dx%d", ok, sz.cols, sz.rows)
	}
}

func TestSizeCoalescerMultipleRegular(t *testing.T) {
	c := newSizeCoalescer()
	c.note(80, 24)
	c.note(100, 30)
	c.note(120, 40)
	sz, ok := c.take()
	if !ok || sz.cols != 120 || sz.rows != 40 {
		t.Fatalf("got %dx%d want 120x40", sz.cols, sz.rows)
	}
}
