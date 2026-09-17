package terminal

import (
	"errors"
	"io"
	"syscall"
)

// errWouldBlock is returned by test writers to simulate EAGAIN.
var errWouldBlock = errors.New("pty: would block")

func isEINTR(err error) bool {
	return errors.Is(err, syscall.EINTR)
}

func isEAGAIN(err error) bool {
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, errWouldBlock)
}

// writeFull writes p to w, advancing only by the number of bytes actually
// accepted. It retries EINTR and optional waitEAGAIN (EAGAIN/EWOULDBLOCK).
// A zero-byte write with no error is treated as a closed peer.
func writeFull(w io.Writer, p []byte, waitEAGAIN func() error) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		if n > 0 {
			p = p[n:]
		}
		if err == nil {
			if n == 0 {
				return io.ErrShortWrite
			}
			continue
		}
		if isEINTR(err) {
			continue
		}
		if isEAGAIN(err) {
			if waitEAGAIN == nil {
				return err
			}
			if werr := waitEAGAIN(); werr != nil {
				return werr
			}
			continue
		}
		return err
	}
	return nil
}

type shortWriter struct {
	buf     []byte
	limit   int
	fail    error
	blocked int
	waits   int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	if w.blocked > 0 {
		w.blocked--
		return 0, errWouldBlock
	}
	if w.fail != nil {
		err := w.fail
		w.fail = nil
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	n := len(p)
	if w.limit > 0 && n > w.limit {
		n = w.limit
	}
	w.buf = append(w.buf, p[:n]...)
	return n, nil
}
