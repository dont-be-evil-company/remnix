package helpparse

import (
	"context"
	"strings"
	"sync"
)

// Cache stores parsed help entities and node summaries keyed by command argv.
type Cache struct {
	mu        sync.Mutex
	byKey     map[string][]Entity
	summaries map[string]string
}

// NewCache returns an empty argv-keyed help cache.
func NewCache() *Cache {
	return &Cache{
		byKey:     make(map[string][]Entity),
		summaries: make(map[string]string),
	}
}

func cacheKey(argv []string) string {
	return strings.Join(argv, "\x00")
}

// Get returns a copy of cached entities for argv.
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

// Put stores entities for argv.
func (c *Cache) Put(argv []string, ents []Entity) {
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

// GetSummary returns a cached node one-liner for argv.
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

// PutSummary stores a node one-liner for argv. An empty string still counts
// as a cache hit so a failed DESCRIPTION probe is not retried.
func (c *Cache) PutSummary(argv []string, summary string) {
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

// ProbeFunc fetches --help text for argv.
type ProbeFunc func(ctx context.Context, argv []string) (string, error)

// FillEmpty probes and parses help when any description is missing.
// Probe errors are ignored so the overlay still shows names.
func FillEmpty(ctx context.Context, prefix string, items, descrs []string, cache *Cache, probe ProbeFunc) []string {
	if !NeedsEnrich(items, descrs) {
		return descrs
	}
	argv := HelpArgv(prefix)
	if len(argv) == 0 {
		return descrs
	}
	ents, ok := cache.Get(argv)
	if !ok {
		if probe == nil {
			probe = Probe
		}
		help, err := probe(ctx, argv)
		if err != nil || help == "" {
			cache.Put(argv, nil)
			cache.PutSummary(argv, "")
			return descrs
		}
		ents = Parse(help)
		cache.Put(argv, ents)
		cache.PutSummary(argv, NodeSummary(help))
	}
	return Enrich(prefix, items, descrs, ents)
}

// FillNodes probes each still-empty overlay row's own help (not the parent
// page) and fills a NAME/DESCRIPTION one-liner. At most limit rows are
// probed, in order, so the visible menu is filled without fetching every
// sibling. onUpdate, if set, is called after each successful fill.
func FillNodes(ctx context.Context, prefix string, items, descrs []string, cache *Cache, probe ProbeFunc, limit int, onUpdate func([]string)) []string {
	out := append([]string(nil), descrs...)
	if len(out) < len(items) {
		out = append(out, make([]string, len(items)-len(out))...)
	}
	if !NeedsEnrich(items, out) {
		return out
	}
	if limit <= 0 {
		limit = 8
	}
	if probe == nil {
		probe = Probe
	}
	parent := HelpArgv(prefix)
	probed := 0
	notify := func() {
		if onUpdate != nil {
			onUpdate(append([]string(nil), out...))
		}
	}
	for i, item := range items {
		if ctx.Err() != nil {
			break
		}
		if strings.TrimSpace(out[i]) != "" {
			continue
		}
		if probed >= limit {
			break
		}
		argv := ItemArgv(prefix, item)
		if len(argv) == 0 || argvEqual(argv, parent) {
			continue
		}
		probed++
		if sum, ok := cache.GetSummary(argv); ok {
			if sum != "" {
				out[i] = sum
				notify()
			}
			continue
		}
		help, err := probe(ctx, argv)
		if err != nil || help == "" {
			cache.Put(argv, nil)
			cache.PutSummary(argv, "")
			continue
		}
		cache.Put(argv, Parse(help))
		sum := NodeSummary(help)
		cache.PutSummary(argv, sum)
		if sum == "" {
			continue
		}
		out[i] = sum
		notify()
	}
	return out
}
