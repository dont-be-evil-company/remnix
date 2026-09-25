package terminal

import "testing"

func TestSizeCoalescerLatestWins(t *testing.T) {
	c := newSizeCoalescer()
	c.note(80, 24, 800, 600)
	c.note(1, 1, 0, 0)
	c.note(80, 24, 800, 600)
	sz, ok := c.take()
	if !ok {
		t.Fatal("expected pending size")
	}
	if sz.cols != 80 || sz.rows != 24 || sz.xpixel != 800 || sz.ypixel != 600 {
		t.Fatalf("got %dx%d %dx%dpx want 80x24 800x600px", sz.cols, sz.rows, sz.xpixel, sz.ypixel)
	}
	if _, ok := c.take(); ok {
		t.Fatal("second take should be empty")
	}
}

func TestSizeCoalescerStableSmall(t *testing.T) {
	c := newSizeCoalescer()
	c.note(1, 1, 0, 0)
	sz, ok := c.take()
	if !ok || sz.cols != 1 || sz.rows != 1 {
		t.Fatalf("stable 1x1 must apply, ok=%v %dx%d", ok, sz.cols, sz.rows)
	}
}

func TestSizeCoalescerMultipleRegular(t *testing.T) {
	c := newSizeCoalescer()
	c.note(80, 24, 0, 0)
	c.note(100, 30, 0, 0)
	c.note(120, 40, 1600, 900)
	sz, ok := c.take()
	if !ok || sz.cols != 120 || sz.rows != 40 || sz.xpixel != 1600 || sz.ypixel != 900 {
		t.Fatalf("got %dx%d %dx%dpx want 120x40 1600x900px", sz.cols, sz.rows, sz.xpixel, sz.ypixel)
	}
}
