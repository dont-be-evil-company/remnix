package terminal

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

func TestPTYInputQueueOrderAndNoDrop(t *testing.T) {
	q := newPTYInputQueue(1024)
	if err := q.push([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if err := q.push([]byte("cd")); err != nil {
		t.Fatal(err)
	}
	a, ok := q.popWait()
	if !ok || string(a) != "ab" {
		t.Fatalf("first %q %v", a, ok)
	}
	b, ok := q.popWait()
	if !ok || string(b) != "cd" {
		t.Fatalf("second %q %v", b, ok)
	}
}

func TestPTYInputQueueSaturatesWithoutDropping(t *testing.T) {
	q := newPTYInputQueue(8)
	if err := q.push([]byte("12345678")); err != nil {
		t.Fatal(err)
	}
	if err := q.push([]byte("x")); err != errPTYInputSaturated {
		t.Fatalf("want saturated, got %v", err)
	}
	got, ok := q.popWait()
	if !ok || string(got) != "12345678" {
		t.Fatalf("must keep original bytes, got %q", got)
	}
}

func TestPTYInputQueueStopUnblocks(t *testing.T) {
	q := newPTYInputQueue(64)
	done := make(chan []byte, 1)
	go func() {
		item, _ := q.popWait()
		done <- item
	}()
	select {
	case <-done:
		t.Fatal("popWait returned before stop")
	case <-time.After(30 * time.Millisecond):
	}
	q.stop()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stop did not unblock")
	}
}

func TestPTYInputQueueConcurrent(t *testing.T) {
	q := newPTYInputQueue(1 << 20)
	var want []byte
	for i := 0; i < 1000; i++ {
		want = append(want, byte(i), byte(i>>8))
	}
	var got []byte
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			item, ok := q.popWait()
			if !ok {
				return
			}
			got = append(got, item...)
		}
	}()
	for i := 0; i < 1000; i++ {
		if err := q.push([]byte{byte(i), byte(i >> 8)}); err != nil {
			t.Fatal(err)
		}
	}
	q.stop()
	wg.Wait()
	if !bytes.Equal(got, want) {
		t.Fatalf("lost or reordered: got %d want %d", len(got), len(want))
	}
}

func TestPTYInputSlowConsumerThenCatchUp(t *testing.T) {
	q := newPTYInputQueue(64)
	defer q.stop()
	var got []byte
	var mu sync.Mutex
	resume := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-resume
		for {
			item, ok := q.popWait()
			if !ok {
				return
			}
			mu.Lock()
			got = append(got, item...)
			mu.Unlock()
		}
	}()
	want := bytes.Repeat([]byte("x"), 64)
	if err := q.push(want); err != nil {
		t.Fatal(err)
	}
	if err := q.push([]byte("y")); err != errPTYInputSaturated {
		t.Fatalf("want saturated while consumer paused, got %v", err)
	}
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("consumer ran early: %q", got)
	}
	mu.Unlock()
	close(resume)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n == len(want) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	q.stop()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if !bytes.Equal(got, want) {
		t.Fatalf("after catch-up got %q want %q", got, want)
	}
}

func TestParseQueueTinyChunksBenchmarkPath(t *testing.T) {
	q := newParseQueue(1 << 16)
	for i := 0; i < 2000; i++ {
		if _, ok := q.enqueue([]byte{byte(i)}, false); !ok {
			t.Fatal("enqueue")
		}
	}
	for i := 0; i < 2000; i++ {
		item, ok := q.pop()
		if !ok || len(item.data) != 1 {
			t.Fatalf("pop %d ok=%v", i, ok)
		}
		q.markParsed(item.seq)
	}
}
