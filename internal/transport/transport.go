package transport

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
)

type Object struct {
	Key  string
	Size int64
}

type HealthState string

const (
	HealthOK               HealthState = "ok"
	HealthOffline          HealthState = "offline"
	HealthAuthRequired     HealthState = "auth_required"
	HealthPermissionDenied HealthState = "permission_denied"
	HealthNotFound         HealthState = "not_found"
	HealthRateLimited      HealthState = "rate_limited"
	HealthMisconfigured    HealthState = "misconfigured"
	HealthCorruptRepo      HealthState = "corrupt_repository"
	HealthUnknown          HealthState = "unknown"
)

type HealthStatus struct {
	State   HealthState
	Message string
	Action  string
}

type Capabilities struct {
	AtomicRename    bool
	Mkdir           bool
	Rename          bool
	RecursiveDelete bool
	VirtualDirs     bool
	// ListingExpensive is true when List/ListDirs of a prefix talks to a
	// remote API and may scan many unrelated objects (cloud backends).
	ListingExpensive bool
}

type Transport interface {
	List(ctx context.Context, prefix string) ([]Object, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Put(ctx context.Context, key string, r io.Reader) error
	PutAtomic(ctx context.Context, key string, r io.Reader) error
	Remove(ctx context.Context, key string) error
	ListDirs(ctx context.Context, prefix string) ([]string, error)
	Mkdir(ctx context.Context, key string) error
	HealthCheck(ctx context.Context) (HealthStatus, error)
	Capabilities() Capabilities
}

// Deduper collapses same-path objects on backends that allow duplicate names
// (Google Drive). Directory and S3-style transports do not implement it.
type Deduper interface {
	Dedupe(ctx context.Context) error
}

func Dedupe(ctx context.Context, tr Transport) error {
	d, ok := tr.(Deduper)
	if !ok {
		return nil
	}
	return d.Dedupe(ctx)
}

// DeleteIfExists removes key. A missing object is success; other backend
// errors are returned unchanged.
func DeleteIfExists(ctx context.Context, tr Transport, key string) error {
	err := tr.Remove(ctx, key)
	if err == nil || errors.Is(err, ErrRemoteNotFound) || errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		return nil
	}
	return err
}

// Session is implemented by rsync/scp-style transports that stage a local copy.
type Session interface {
	Begin(ctx context.Context) error
	End(ctx context.Context) error
}

// WithSession pulls a session transport, runs fn, then pushes. Read-only callers
// that must not push should use PullSession instead.
func WithSession(ctx context.Context, tr Transport, fn func() error) error {
	s, ok := tr.(Session)
	if !ok {
		return fn()
	}
	if err := s.Begin(ctx); err != nil {
		return err
	}
	err := fn()
	endErr := s.End(ctx)
	if err != nil {
		return err
	}
	return endErr
}

// PullSession calls Begin when the transport stages remotely, without End/push.
func PullSession(ctx context.Context, tr Transport) error {
	s, ok := tr.(Session)
	if !ok {
		return nil
	}
	return s.Begin(ctx)
}
