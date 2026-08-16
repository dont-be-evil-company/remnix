package transport

import (
	"context"
	"io"
)

type Object struct {
	Key  string
	Size int64
}

type Transport interface {
	List(ctx context.Context, prefix string) ([]Object, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, r io.Reader) error
	PutAtomic(ctx context.Context, key string, r io.Reader) error
	Remove(ctx context.Context, key string) error
}
