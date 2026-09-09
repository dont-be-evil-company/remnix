package equalize

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dont-be-evil-company/remnix/internal/progress"
	"github.com/dont-be-evil-company/remnix/internal/sync/gc"
	"github.com/dont-be-evil-company/remnix/internal/transport"
)

type Named struct {
	ID        string
	Transport transport.Transport
}

func Equalize(ctx context.Context, endpoints []Named) error {
	if len(endpoints) < 2 {
		return nil
	}
	progress.Report(ctx, "mirroring endpoints")
	var closers []func()
	defer func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}()
	for _, ep := range endpoints {
		if s, ok := ep.Transport.(transport.Session); ok {
			if err := s.Begin(ctx); err != nil {
				return fmt.Errorf("%s: %w", ep.ID, err)
			}
			s := s
			closers = append(closers, func() { _ = s.End(ctx) })
		}
	}

	have := make([]map[string]bool, len(endpoints))
	var errs []error
	for i, ep := range endpoints {
		objs, err := gc.ListLayoutObjects(ctx, ep.Transport)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: list: %w", ep.ID, err))
			have[i] = map[string]bool{}
			continue
		}
		have[i] = map[string]bool{}
		for _, o := range objs {
			if skipKey(o.Key) {
				continue
			}
			have[i][o.Key] = true
		}
	}

	union := map[string]bool{}
	for _, keys := range have {
		for k := range keys {
			union[k] = true
		}
	}

	for key := range union {
		var sources []int
		var dests []int
		for i := range endpoints {
			if have[i][key] {
				sources = append(sources, i)
			} else {
				dests = append(dests, i)
			}
		}
		if len(sources) == 0 || len(dests) == 0 {
			continue
		}
		src := endpoints[sources[0]]
		raw, err := readAll(ctx, src.Transport, key)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: get %s: %w", src.ID, key, err))
			continue
		}
		srcCounter := int64(-1)
		if key == "metadata/manifest" {
			srcCounter = manifestCounter(raw)
		}
		for _, di := range dests {
			dest := endpoints[di]
			if key == "metadata/manifest" && srcCounter >= 0 {
				destRaw, err := readAll(ctx, dest.Transport, key)
				if err == nil {
					dstCounter := manifestCounter(destRaw)
					if srcCounter <= dstCounter {
						continue
					}
				}
			}
			if err := dest.Transport.PutAtomic(ctx, key, bytes.NewReader(raw)); err != nil {
				errs = append(errs, fmt.Errorf("%s: put %s: %w", dest.ID, key, err))
			}
		}
	}
	return errors.Join(errs...)
}

func skipKey(key string) bool {
	base := key
	if i := strings.LastIndex(key, "/"); i >= 0 {
		base = key[i+1:]
	}
	if strings.Contains(base, ".tmp-") || strings.HasSuffix(base, ".tmp") {
		return true
	}
	return strings.HasPrefix(key, "metadata/health/")
}

func manifestCounter(raw []byte) int64 {
	var rm struct {
		Counter int64 `json:"counter"`
	}
	if json.Unmarshal(raw, &rm) != nil {
		return -1
	}
	return rm.Counter
}

func readAll(ctx context.Context, tr transport.Transport, key string) ([]byte, error) {
	r, err := tr.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
