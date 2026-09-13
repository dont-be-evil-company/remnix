package terminal

import "sync"

type terminalSize struct {
	cols int
	rows int
}

// sizeCoalescer keeps one pending resize. A newer size replaces an older one
// so obsolete intermediate geometries are never applied.
type sizeCoalescer struct {
	mu      sync.Mutex
	pending *terminalSize
	sig     chan struct{}
}

func newSizeCoalescer() *sizeCoalescer {
	return &sizeCoalescer{sig: make(chan struct{}, 1)}
}

func (c *sizeCoalescer) note(cols, rows int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.pending = &terminalSize{cols: cols, rows: rows}
	c.mu.Unlock()
	select {
	case c.sig <- struct{}{}:
	default:
	}
}

func (c *sizeCoalescer) take() (terminalSize, bool) {
	if c == nil {
		return terminalSize{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil {
		return terminalSize{}, false
	}
	sz := *c.pending
	c.pending = nil
	return sz, true
}
