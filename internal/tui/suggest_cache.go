package tui

import (
	"strings"
	"sync"
)

// SuggestCache stores completion lists keyed by the prefix used to fetch them.
// A daemon session can reuse one cache across overlay continue RPCs.
type SuggestCache struct {
	mu       sync.Mutex
	byPrefix map[string][]suggestItem
}

// NewSuggestCache returns an empty prefix-keyed completion cache.
func NewSuggestCache() *SuggestCache {
	return &SuggestCache{byPrefix: make(map[string][]suggestItem)}
}

// Store records items fetched for prefix. Empty lists are stored so a later
// Ctrl+Space on the same prefix does not re-query.
func (c *SuggestCache) Store(prefix string, items, descrs []string) {
	c.store(prefix, suggestItemsFrom(items, descrs))
}

func (c *SuggestCache) store(prefix string, items []suggestItem) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.byPrefix == nil {
		c.byPrefix = make(map[string][]suggestItem)
	}
	c.byPrefix[prefix] = append([]suggestItem(nil), items...)
}

// Has reports whether prefix, or prefix with a trailing space added/removed,
// already has a cached list.
func (c *SuggestCache) Has(prefix string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.byPrefix[prefix]; ok {
		return true
	}
	if strings.HasSuffix(prefix, " ") {
		_, ok := c.byPrefix[strings.TrimRight(prefix, " ")]
		return ok
	}
	_, ok := c.byPrefix[prefix+" "]
	return ok
}

func (c *SuggestCache) best(typed string) (key string, items []suggestItem, ok bool) {
	if c == nil {
		return "", nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	bestLen := -1
	for k, v := range c.byPrefix {
		if !strings.HasPrefix(typed, k) {
			continue
		}
		if len(k) > bestLen {
			bestLen = len(k)
			key = k
			items = v
		}
	}
	if bestLen < 0 {
		return "", nil, false
	}
	return key, append([]suggestItem(nil), items...), true
}

func suggestItemsFrom(items, descrs []string) []suggestItem {
	all := make([]suggestItem, 0, len(items))
	for i, cmd := range items {
		if cmd == "" {
			continue
		}
		d := ""
		if i < len(descrs) {
			d = strings.TrimSpace(descrs[i])
		}
		all = append(all, suggestItem{cmd: cmd, descr: d})
	}
	return all
}
