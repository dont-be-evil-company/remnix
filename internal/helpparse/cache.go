package helpparse

import (
	"context"
	"log/slog"
	"strings"
	"sync"
)

const nodeProbeJobs = 4

// Durable is an optional persistent backend for parsed help pages.
// A hit (including an empty summary) means the overlay must not probe.
type Durable interface {
	Load(argv []string) (ents []Entity, summary string, ok bool)
	LoadMany(argvs [][]string) map[string]DurablePage
	Save(argv []string, ents []Entity, summary string) error
}

// DurablePage is one L2 help page (entities + node summary).
type DurablePage struct {
	Entities []Entity
	Summary  string
}

// Cache stores parsed help entities and node summaries keyed by command argv.
type Cache struct {
	mu        sync.Mutex
	byKey     map[string][]Entity
	summaries map[string]string
	durable   Durable
}

// NewCache returns an empty argv-keyed help cache.
func NewCache() *Cache {
	return &Cache{
		byKey:     make(map[string][]Entity),
		summaries: make(map[string]string),
	}
}

// SetDurable attaches an L2 store. Session L1 is hydrated from it on lookup.
func (c *Cache) SetDurable(d Durable) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.durable = d
	c.mu.Unlock()
}

func (c *Cache) durableStore() Durable {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.durable
}

// ArgvKey is the stable cache / SQLite key for a command path.
func ArgvKey(argv []string) string {
	return strings.Join(argv, "\x00")
}

func cacheKey(argv []string) string {
	return ArgvKey(argv)
}

// Get returns a copy of cached entities for argv (L1 only).
func (c *Cache) Get(argv []string) ([]Entity, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ents, ok := c.byKey[cacheKey(argv)]
	if !ok {
		return nil, false
	}
	return append([]Entity(nil), ents...), true
}

// Put stores entities for argv in L1. Persistence happens via persistPage.
func (c *Cache) Put(argv []string, ents []Entity) {
	c.putMem(argv, ents)
}

func (c *Cache) putMem(argv []string, ents []Entity) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byKey == nil {
		c.byKey = make(map[string][]Entity)
	}
	c.byKey[cacheKey(argv)] = append([]Entity(nil), ents...)
}

// GetSummary returns a cached node one-liner for argv (L1 only).
// ok is true after a probe, even when the summary is empty.
func (c *Cache) GetSummary(argv []string) (string, bool) {
	if c == nil {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.summaries[cacheKey(argv)]
	return s, ok
}

// PutSummary stores a node one-liner for argv in L1.
func (c *Cache) PutSummary(argv []string, summary string) {
	c.putSummaryMem(argv, summary)
}

func (c *Cache) putSummaryMem(argv []string, summary string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.summaries == nil {
		c.summaries = make(map[string]string)
	}
	c.summaries[cacheKey(argv)] = summary
}

func (c *Cache) hydrate(argv []string, ents []Entity, summary string) {
	c.putMem(argv, ents)
	c.putSummaryMem(argv, summary)
}

// ProbeFunc fetches --help text for argv.
type ProbeFunc func(ctx context.Context, argv []string) (string, error)

type probeResult struct {
	ents    []Entity
	summary string
}

var probeFlight flight

type flight struct {
	mu sync.Mutex
	m  map[string]*flightCall
}

type flightCall struct {
	wg  sync.WaitGroup
	res probeResult
}

func (f *flight) do(key string, fn func() probeResult) probeResult {
	f.mu.Lock()
	if f.m == nil {
		f.m = map[string]*flightCall{}
	}
	if c, ok := f.m[key]; ok {
		f.mu.Unlock()
		c.wg.Wait()
		return c.res
	}
	c := &flightCall{}
	c.wg.Add(1)
	f.m[key] = c
	f.mu.Unlock()
	c.res = fn()
	f.mu.Lock()
	delete(f.m, key)
	f.mu.Unlock()
	c.wg.Done()
	return c.res
}

func lookupPage(cache *Cache, argv []string) (ents []Entity, summary string, ok bool) {
	if ents, hit := cache.Get(argv); hit {
		sum, _ := cache.GetSummary(argv)
		return ents, sum, true
	}
	if sum, hit := cache.GetSummary(argv); hit {
		return nil, sum, true
	}
	d := cache.durableStore()
	if d == nil {
		return nil, "", false
	}
	ents, summary, ok = d.Load(argv)
	if !ok {
		return nil, "", false
	}
	cache.hydrate(argv, ents, summary)
	return ents, summary, true
}

func persistPage(cache *Cache, argv []string, ents []Entity, summary string) {
	cache.hydrate(argv, ents, summary)
	d := cache.durableStore()
	if d == nil {
		return
	}
	if err := d.Save(argv, ents, summary); err != nil {
		slog.Error("remnix suggest cache: save", "err", err, "argv", strings.Join(argv, " "))
	}
}

func probeAndPersist(ctx context.Context, cache *Cache, argv []string, probe ProbeFunc) probeResult {
	key := ArgvKey(argv)
	return probeFlight.do(key, func() probeResult {
		if ents, sum, ok := lookupPage(cache, argv); ok {
			return probeResult{ents: ents, summary: sum}
		}
		if probe == nil {
			probe = Probe
		}
		help, err := probe(ctx, argv)
		if err != nil || help == "" {
			if ctx.Err() != nil {
				return probeResult{}
			}
			persistPage(cache, argv, nil, "")
			return probeResult{}
		}
		ents := Parse(help)
		sum := NodeSummary(help)
		persistPage(cache, argv, ents, sum)
		return probeResult{ents: ents, summary: sum}
	})
}

// FillEmpty probes and parses help when any description is missing.
// Probe errors are ignored so the overlay still shows names.
// L2 hits never probe; a probe always writebacks when Durable is set.
func FillEmpty(ctx context.Context, prefix string, items, descrs []string, cache *Cache, probe ProbeFunc) []string {
	if !NeedsEnrich(items, descrs) {
		return descrs
	}
	argv := HelpArgv(prefix)
	if len(argv) == 0 {
		return descrs
	}
	ents, _, ok := lookupPage(cache, argv)
	if !ok {
		ents = probeAndPersist(ctx, cache, argv, probe).ents
	}
	return Enrich(prefix, items, descrs, ents)
}

// FillNodes probes each still-empty overlay row's own help (not the parent
// page) and fills a NAME/DESCRIPTION one-liner. Lookups from L1/L2 do not
// count toward limit. limit < 1 probes every missing row; otherwise at most
// that many cold-cache probes run. onUpdate, if set, is called after each
// successful fill.
func FillNodes(ctx context.Context, prefix string, items, descrs []string, cache *Cache, probe ProbeFunc, limit int, onUpdate func([]string)) []string {
	out := append([]string(nil), descrs...)
	if len(out) < len(items) {
		out = append(out, make([]string, len(items)-len(out))...)
	}
	if !NeedsEnrich(items, out) {
		return out
	}
	parent := HelpArgv(prefix)
	notify := func() {
		if onUpdate != nil {
			onUpdate(append([]string(nil), out...))
		}
	}

	type row struct {
		i    int
		argv []string
	}
	var missing []row
	for i, item := range items {
		if strings.TrimSpace(out[i]) != "" {
			continue
		}
		argv := ItemArgv(prefix, item)
		if len(argv) == 0 || argvEqual(argv, parent) {
			continue
		}
		if sum, ok := cache.GetSummary(argv); ok {
			if sum != "" {
				out[i] = sum
				notify()
			}
			continue
		}
		missing = append(missing, row{i: i, argv: argv})
	}

	if d := cache.durableStore(); d != nil && len(missing) > 0 {
		argvs := make([][]string, len(missing))
		for i, m := range missing {
			argvs[i] = m.argv
		}
		pages := d.LoadMany(argvs)
		still := missing[:0]
		for _, m := range missing {
			page, ok := pages[ArgvKey(m.argv)]
			if !ok {
				still = append(still, m)
				continue
			}
			cache.hydrate(m.argv, page.Entities, page.Summary)
			if page.Summary != "" {
				out[m.i] = page.Summary
				notify()
			}
		}
		missing = still
	}

	if limit > 0 && len(missing) > limit {
		missing = missing[:limit]
	}
	if len(missing) == 0 {
		return out
	}

	var mu sync.Mutex
	jobs := nodeProbeJobs
	if jobs > len(missing) {
		jobs = len(missing)
	}
	next := 0
	var wg sync.WaitGroup
	wg.Add(jobs)
	for i := 0; i < jobs; i++ {
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				mu.Lock()
				if next >= len(missing) {
					mu.Unlock()
					return
				}
				m := missing[next]
				next++
				mu.Unlock()

				res := probeAndPersist(ctx, cache, m.argv, probe)
				if res.summary == "" {
					continue
				}
				mu.Lock()
				out[m.i] = res.summary
				snap := append([]string(nil), out...)
				mu.Unlock()
				if onUpdate != nil {
					onUpdate(snap)
				}
			}
		}()
	}
	wg.Wait()
	return out
}
