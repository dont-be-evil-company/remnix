package tui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type Status struct {
	mu   sync.Mutex
	msg  string
	out  io.Writer
	tty  bool
	once sync.Once
	stop chan struct{}
	done chan struct{}
}

func StartStatus(w io.Writer) *Status {
	if w == nil {
		w = os.Stderr
	}
	s := &Status{
		out:  w,
		tty:  writerIsTTY(w),
		stop: make(chan struct{}),
		done: make(chan struct{}),
		msg:  "working",
	}
	if s.tty {
		_, _ = fmt.Fprint(s.out, "\033[?25l")
		go s.loop()
	}
	return s
}

func (s *Status) Set(msg string) {
	if s == nil || msg == "" {
		return
	}
	s.mu.Lock()
	dup := msg == s.msg
	s.msg = msg
	s.mu.Unlock()
	if !s.tty && !dup {
		_, _ = fmt.Fprintln(s.out, msg)
	}
}

func (s *Status) Finish(err error) {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.stop)
		if s.tty {
			<-s.done
			_, _ = fmt.Fprint(s.out, "\r\033[K\033[?25h")
			if err == nil {
				_, _ = fmt.Fprintln(s.out, "✓ synced")
			}
			return
		}
		if err == nil {
			_, _ = fmt.Fprintln(s.out, "synced")
		}
	})
}

func (s *Status) loop() {
	defer close(s.done)
	tick := time.NewTicker(80 * time.Millisecond)
	defer tick.Stop()
	i := 0
	for {
		select {
		case <-s.stop:
			return
		case <-tick.C:
			s.mu.Lock()
			msg := s.msg
			s.mu.Unlock()
			_, _ = fmt.Fprintf(s.out, "\r\033[K%s %s", spinnerFrames[i%len(spinnerFrames)], msg)
			i++
		}
	}
}

func writerIsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}
