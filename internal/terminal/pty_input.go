package terminal

import (
	"errors"
	"sync"
)

// Keyboard input is authoritative: this queue must never drop bytes.
// Shadow VT parsing uses parseQueue, which may discard stale backlog.
const maxPTYInputBytes = 1 << 20

var (
	errPTYInputStopped   = errors.New("pty input queue stopped")
	errPTYInputSaturated = errors.New("pty input queue saturated")
)

// ptyInputQueue is a lossless, bounded FIFO for user bytes heading to the
// child PTY. One producer (socket reader) and one consumer (PTY writer).
// Exhaustion is an error, never drop-oldest or drop-newest.
type ptyInputQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	chunks   [][]byte
	head     int
	bytes    int
	budget   int
	maxDepth int
	stopped  bool
}

func newPTYInputQueue(budget int) *ptyInputQueue {
	if budget < 1 {
		budget = maxPTYInputBytes
	}
	q := &ptyInputQueue{budget: budget}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *ptyInputQueue) push(p []byte) error {
	if q == nil || len(p) == 0 {
		return nil
	}
	cp := append([]byte(nil), p...)
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		return errPTYInputStopped
	}
	if q.bytes+len(cp) > q.budget {
		if ptyDiagEnabled() {
			ptyDiag.inputSaturated.Add(1)
		}
		return errPTYInputSaturated
	}
	q.chunks = append(q.chunks, cp)
	q.bytes += len(cp)
	if q.bytes > q.maxDepth {
		q.maxDepth = q.bytes
	}
	if ptyDiagEnabled() {
		ptyDiag.inputQueued.Add(uint64(len(cp)))
		diagAddMax(&ptyDiag.inputMaxDepth, uint64(q.maxDepth))
	}
	q.cond.Signal()
	return nil
}

func (q *ptyInputQueue) popWait() ([]byte, bool) {
	if q == nil {
		return nil, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.head >= len(q.chunks) && !q.stopped {
		q.cond.Wait()
	}
	if q.head >= len(q.chunks) {
		return nil, false
	}
	item := q.chunks[q.head]
	q.chunks[q.head] = nil
	q.head++
	q.bytes -= len(item)
	if q.bytes < 0 {
		q.bytes = 0
	}
	if q.head > 32 && q.head*2 >= len(q.chunks) {
		q.chunks = append([][]byte(nil), q.chunks[q.head:]...)
		q.head = 0
	}
	return item, true
}

func (q *ptyInputQueue) stop() {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.stopped = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *ptyInputQueue) depth() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.bytes
}
