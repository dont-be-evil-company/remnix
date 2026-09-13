package terminal

import "sync"

// parseQueue is a bounded, non-blocking backlog for shadow VT parsing.
//
// The real PTY path never waits on this queue. When enqueue would exceed the
// byte budget, the unparsed backlog is discarded, the newest chunk is kept if
// it fits, and dirty is set. remnix cannot reconstruct dropped bytes from the
// attach client, so a later Snapshot/overlay barrier returns best-effort
// emulator state after draining whatever remains. Callers must not treat that
// snapshot as an exact replica of the live terminal.
const maxParseQueuedBytes = 1 << 20

// parseQueueBudget is the live enqueue limit. Tests may lower it; production
// uses maxParseQueuedBytes.
var parseQueueBudget = maxParseQueuedBytes

type parseItem struct {
	seq  uint64
	data []byte
	cpr  bool
}

type parseQueue struct {
	mu          sync.Mutex
	cond        *sync.Cond
	items       []parseItem
	queuedBytes int
	enqueuedSeq uint64
	parsedSeq   uint64
	dirty       bool
	stopped     bool
	budget      int
	maxQueued   int
	saturations uint64
}

type parseStats struct {
	queuedBytes int
	maxQueued   int
	saturations uint64
	dirty       bool
	enqueued    uint64
	parsed      uint64
	stopped     bool
}

func newParseQueue(budget int) *parseQueue {
	if budget < 1 {
		budget = maxParseQueuedBytes
	}
	q := &parseQueue{budget: budget}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *parseQueue) enqueue(data []byte, cpr bool) (seq uint64, ok bool) {
	if q == nil || (len(data) == 0 && !cpr) {
		return 0, false
	}
	cp := append([]byte(nil), data...)
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.stopped {
		return q.enqueuedSeq, false
	}
	if q.queuedBytes+len(cp) > q.budget {
		q.saturations++
		q.dirty = true
		q.items = q.items[:0]
		q.queuedBytes = 0
		q.parsedSeq = q.enqueuedSeq
		q.cond.Broadcast()
		if len(cp) > q.budget {
			return q.enqueuedSeq, false
		}
	}
	q.enqueuedSeq++
	seq = q.enqueuedSeq
	q.items = append(q.items, parseItem{seq: seq, data: cp, cpr: cpr})
	q.queuedBytes += len(cp)
	if q.queuedBytes > q.maxQueued {
		q.maxQueued = q.queuedBytes
	}
	return seq, true
}

func (q *parseQueue) pop() (parseItem, bool) {
	if q == nil {
		return parseItem{}, false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return parseItem{}, false
	}
	item := q.items[0]
	n := copy(q.items, q.items[1:])
	q.items = q.items[:n]
	q.queuedBytes -= len(item.data)
	if q.queuedBytes < 0 {
		q.queuedBytes = 0
	}
	return item, true
}

func (q *parseQueue) markParsed(seq uint64) {
	if q == nil {
		return
	}
	q.mu.Lock()
	if seq > q.parsedSeq {
		q.parsedSeq = seq
	}
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *parseQueue) currentSeq() uint64 {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.enqueuedSeq
}

// wait blocks until parsedSeq >= target or the queue is stopped.
func (q *parseQueue) wait(target uint64) {
	if q == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.parsedSeq < target && !q.stopped {
		q.cond.Wait()
	}
}

func (q *parseQueue) stop() {
	if q == nil {
		return
	}
	q.mu.Lock()
	q.stopped = true
	q.items = nil
	q.queuedBytes = 0
	q.parsedSeq = q.enqueuedSeq
	q.cond.Broadcast()
	q.mu.Unlock()
}

func (q *parseQueue) stats() parseStats {
	if q == nil {
		return parseStats{}
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return parseStats{
		queuedBytes: q.queuedBytes,
		maxQueued:   q.maxQueued,
		saturations: q.saturations,
		dirty:       q.dirty,
		enqueued:    q.enqueuedSeq,
		parsed:      q.parsedSeq,
		stopped:     q.stopped,
	}
}
