package terminal

import (
	"bytes"
	"testing"
	"time"
)

func TestParseQueueBarrierWaitsForPriorItems(t *testing.T) {
	q := newParseQueue(1024)
	seqA, ok := q.enqueue([]byte("A"), false)
	if !ok {
		t.Fatal("enqueue A")
	}
	seqB, ok := q.enqueue([]byte("B"), false)
	if !ok {
		t.Fatal("enqueue B")
	}
	done := make(chan struct{})
	go func() {
		q.wait(seqB)
		close(done)
	}()
	item, ok := q.pop()
	if !ok || item.seq != seqA {
		t.Fatalf("pop A: %+v %v", item, ok)
	}
	q.markParsed(item.seq)
	select {
	case <-done:
		t.Fatal("wait(B) returned before B was parsed")
	case <-time.After(50 * time.Millisecond):
	}
	item, ok = q.pop()
	if !ok || item.seq != seqB {
		t.Fatalf("pop B: %+v %v", item, ok)
	}
	q.markParsed(item.seq)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wait(B) did not return after B was parsed")
	}
}

func TestParseQueueBarrierIgnoresLaterItems(t *testing.T) {
	q := newParseQueue(1024)
	seqA, _ := q.enqueue([]byte("A"), false)
	done := make(chan struct{})
	go func() {
		q.wait(seqA)
		close(done)
	}()
	item, _ := q.pop()
	q.enqueue([]byte("B"), false)
	select {
	case <-done:
		t.Fatal("wait(A) returned before A was parsed")
	case <-time.After(30 * time.Millisecond):
	}
	q.markParsed(item.seq)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wait(A) waited for B")
	}
	st := q.stats()
	if st.queuedBytes != 1 {
		t.Fatalf("B should still be queued, queuedBytes=%d", st.queuedBytes)
	}
}

func TestParseQueuePopWaitWakesOnEnqueue(t *testing.T) {
	q := newParseQueue(1024)
	done := make(chan parseItem, 1)
	go func() {
		item, ok := q.popWait()
		if ok {
			done <- item
		}
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("popWait returned before enqueue")
	case <-time.After(30 * time.Millisecond):
	}
	if _, ok := q.enqueue([]byte("Z"), false); !ok {
		t.Fatal("enqueue")
	}
	select {
	case item := <-done:
		if string(item.data) != "Z" {
			t.Fatalf("got %q", item.data)
		}
	case <-time.After(time.Second):
		q.stop()
		t.Fatal("popWait did not wake on enqueue")
	}
	q.stop()
}

func TestParseQueueStopUnblocksWait(t *testing.T) {
	q := newParseQueue(1024)
	seq, _ := q.enqueue([]byte("A"), false)
	done := make(chan struct{})
	go func() {
		q.wait(seq)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("wait returned before stop")
	case <-time.After(30 * time.Millisecond):
	}
	q.stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop did not unblock wait")
	}
	if _, ok := q.enqueue([]byte("Z"), false); ok {
		t.Fatal("enqueue after stop")
	}
}

func TestParseQueueBoundsMemory(t *testing.T) {
	q := newParseQueue(64)
	a := bytes.Repeat([]byte("a"), 40)
	b := bytes.Repeat([]byte("b"), 40)
	if _, ok := q.enqueue(a, false); !ok {
		t.Fatal("first enqueue should fit")
	}
	if _, ok := q.enqueue(b, false); !ok {
		t.Fatal("overflow should keep the newest chunk")
	}
	st := q.stats()
	if st.queuedBytes > 64 {
		t.Fatalf("queuedBytes %d exceeds budget 64", st.queuedBytes)
	}
	if st.queuedBytes != 40 {
		t.Fatalf("queuedBytes %d want 40 (newest chunk)", st.queuedBytes)
	}
	if !st.dirty || st.saturations < 1 {
		t.Fatalf("dirty=%v saturations=%d", st.dirty, st.saturations)
	}
	item, ok := q.pop()
	if !ok || string(item.data) != string(b) {
		t.Fatalf("expected newest chunk, got %q ok=%v", item.data, ok)
	}
}

func TestParseQueueDoesNotCopyCallersBuffer(t *testing.T) {
	q := newParseQueue(1024)
	buf := []byte("hello")
	q.enqueue(buf, false)
	buf[0] = 'x'
	item, ok := q.pop()
	if !ok || string(item.data) != "hello" {
		t.Fatalf("queued slice shared backing array: %q", item.data)
	}
}

func BenchmarkParseQueueEnqueue(b *testing.B) {
	q := newParseQueue(maxParseQueuedBytes)
	chunk := make([]byte, 1024)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := q.enqueue(chunk, false); !ok {
			b.Fatal("enqueue rejected")
		}
		item, ok := q.pop()
		if !ok {
			b.Fatal("pop")
		}
		q.markParsed(item.seq)
	}
}

func BenchmarkParseQueueTinyChunks(b *testing.B) {
	q := newParseQueue(maxParseQueuedBytes)
	chunk := []byte{'x'}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := q.enqueue(chunk, false); !ok {
			b.Fatal("enqueue rejected")
		}
		item, ok := q.pop()
		if !ok {
			b.Fatal("pop")
		}
		q.markParsed(item.seq)
	}
}
