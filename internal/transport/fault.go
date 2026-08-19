package transport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
	"time"
)

// FaultTransport wraps a Transport for tests.
type FaultTransport struct {
	Base                Transport
	FailWriteAfterBytes int64
	FailDelete          bool
	StaleList           bool
	CorruptRead         bool
	Delay               time.Duration
	mu                  sync.Mutex
	stale               []Object
}

var _ Transport = (*FaultTransport)(nil)

func (f *FaultTransport) List(ctx context.Context, prefix string) ([]Object, error) {
	if f.Delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.Delay):
		}
	}
	objs, err := f.Base.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.StaleList && f.stale != nil {
		return append([]Object(nil), f.stale...), nil
	}
	f.stale = append([]Object(nil), objs...)
	return objs, nil
}

func (f *FaultTransport) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, err := f.Base.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if f.CorruptRead {
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		if len(data) > 0 {
			data[0] ^= 0xff
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	return rc, nil
}

func (f *FaultTransport) Put(ctx context.Context, key string, r io.Reader) error {
	return f.put(ctx, key, r, false)
}

func (f *FaultTransport) PutAtomic(ctx context.Context, key string, r io.Reader) error {
	return f.put(ctx, key, r, true)
}

func (f *FaultTransport) put(ctx context.Context, key string, r io.Reader, atomic bool) error {
	if f.FailWriteAfterBytes > 0 {
		limited := &limitedReader{R: r, N: f.FailWriteAfterBytes}
		if atomic {
			err := f.Base.PutAtomic(ctx, key, limited)
			if limited.hit {
				return fmt.Errorf("fault: write truncated after %d bytes", f.FailWriteAfterBytes)
			}
			return err
		}
		err := f.Base.Put(ctx, key, limited)
		if limited.hit {
			return fmt.Errorf("fault: write truncated after %d bytes", f.FailWriteAfterBytes)
		}
		return err
	}
	if atomic {
		return f.Base.PutAtomic(ctx, key, r)
	}
	return f.Base.Put(ctx, key, r)
}

func (f *FaultTransport) Remove(ctx context.Context, key string) error {
	if f.FailDelete {
		return fmt.Errorf("fault: delete failed")
	}
	return f.Base.Remove(ctx, key)
}

func (f *FaultTransport) ListDirs(ctx context.Context, prefix string) ([]string, error) {
	return f.Base.ListDirs(ctx, prefix)
}

func (f *FaultTransport) ListShallow(ctx context.Context, prefix string) ([]Object, error) {
	if s, ok := f.Base.(interface {
		ListShallow(context.Context, string) ([]Object, error)
	}); ok {
		return s.ListShallow(ctx, prefix)
	}
	return nil, nil
}

func (f *FaultTransport) Mkdir(ctx context.Context, key string) error {
	return f.Base.Mkdir(ctx, key)
}

func (f *FaultTransport) HealthCheck(ctx context.Context) (HealthStatus, error) {
	return f.Base.HealthCheck(ctx)
}

func (f *FaultTransport) Capabilities() Capabilities {
	return f.Base.Capabilities()
}

type limitedReader struct {
	R   io.Reader
	N   int64
	hit bool
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.N <= 0 {
		l.hit = true
		return 0, fmt.Errorf("fault: write truncated")
	}
	if int64(len(p)) > l.N {
		p = p[:l.N]
	}
	n, err := l.R.Read(p)
	l.N -= int64(n)
	return n, err
}
