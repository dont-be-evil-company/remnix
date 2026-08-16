package history

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"
)

type CacheStats struct {
	Entries         int
	UniqueCommands  int
	InternedStrings int
	Bytes           int64
	Dirty           bool
	RowsScanned     int
	BuildDuration   time.Duration
	HeapBefore      uint64
	HeapAfter       uint64
}

type Cache struct {
	mu      sync.RWMutex
	pool    *stringPool
	recs    []cacheRec
	byCmd   map[int32]int
	idToCmd map[string]int32
	sorted  []int
	dirty   bool
	stats   CacheStats
}

type cacheRec struct {
	cmd, cwd, device, session int32
	lastStart                 int64
	lastExit                  int32
	freq                      int32
}

type stringPool struct {
	strs []string
	ids  map[string]int32
}

func newStringPool() *stringPool {
	return &stringPool{ids: make(map[string]int32)}
}

func (p *stringPool) intern(s string) int32 {
	if id, ok := p.ids[s]; ok {
		return id
	}
	id := int32(len(p.strs))
	p.strs = append(p.strs, s)
	p.ids[s] = id
	return id
}

func (p *stringPool) get(id int32) string {
	if id < 0 || int(id) >= len(p.strs) {
		return ""
	}
	return p.strs[id]
}

func NewCache() *Cache {
	return &Cache{
		pool:    newStringPool(),
		byCmd:   make(map[int32]int),
		idToCmd: make(map[string]int32),
	}
}

func (c *Cache) Rebuild(ctx context.Context, store *Store) error {
	start := time.Now()
	next := NewCache()
	n := 0
	err := store.ScanLive(func(e Entry) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		n++
		next.applyCreatedLocked(e)
		return nil
	})
	if err != nil {
		return err
	}
	next.resortLocked()
	next.stats.RowsScanned = n
	next.stats.BuildDuration = time.Since(start)
	next.stats.UniqueCommands = len(next.recs)
	next.stats.Entries = len(next.idToCmd)
	next.stats.Bytes = next.estimateBytesLocked()

	c.mu.Lock()
	c.pool = next.pool
	c.recs = next.recs
	c.byCmd = next.byCmd
	c.idToCmd = next.idToCmd
	c.sorted = next.sorted
	c.dirty = false
	c.stats = next.stats
	c.stats.InternedStrings = len(c.pool.strs)
	c.mu.Unlock()
	return nil
}

func (c *Cache) Suggest(prefix string) []Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if prefix == "" || c.dirty {
		return nil
	}
	return c.prefixEntriesLocked(prefix)
}

func (c *Cache) AllUnique() []Entry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.dirty {
		return nil
	}
	entries := make([]Entry, 0, len(c.recs))
	for _, rec := range c.recs {
		entries = append(entries, c.entryFromLocked(rec))
	}
	return entries
}

func (c *Cache) ApplyCreated(e Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.applyCreatedLocked(e)
	c.resortLocked()
	c.maybeCompactLocked()
	c.refreshStatsLocked()
}

func (c *Cache) ApplyCompleted(id string, end time.Time, exit int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cmdID, ok := c.idToCmd[id]
	if !ok {
		return
	}
	idx, ok := c.byCmd[cmdID]
	if !ok {
		return
	}
	c.recs[idx].lastExit = int32(exit)
}

func (c *Cache) ApplyTombstoned(ids []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		cmdID, ok := c.idToCmd[id]
		if !ok {
			c.dirty = true
			continue
		}
		delete(c.idToCmd, id)
		idx, ok := c.byCmd[cmdID]
		if !ok {
			continue
		}
		c.recs[idx].freq--
		if c.recs[idx].freq <= 0 {
			c.removeAtLocked(idx)
		}
	}
	c.resortLocked()
	c.maybeCompactLocked()
	c.refreshStatsLocked()
}

func (c *Cache) ApplyTombstoneCommand(command string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cmdID, ok := c.pool.ids[command]
	if !ok {
		return
	}
	idx, ok := c.byCmd[cmdID]
	if !ok {
		return
	}
	for id, cid := range c.idToCmd {
		if cid == cmdID {
			delete(c.idToCmd, id)
		}
	}
	c.removeAtLocked(idx)
	c.resortLocked()
	c.maybeCompactLocked()
	c.refreshStatsLocked()
}

func (c *Cache) ApplyBatch(cs ChangeSet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range cs.Created {
		c.applyCreatedLocked(e)
	}
	for _, done := range cs.Completed {
		if cmdID, ok := c.idToCmd[done.ID]; ok {
			if idx, ok := c.byCmd[cmdID]; ok {
				c.recs[idx].lastExit = int32(done.Exit)
			}
		}
	}
	for _, id := range cs.Tombstoned {
		cmdID, ok := c.idToCmd[id]
		if !ok {
			c.dirty = true
			continue
		}
		delete(c.idToCmd, id)
		if idx, ok := c.byCmd[cmdID]; ok {
			c.recs[idx].freq--
			if c.recs[idx].freq <= 0 {
				c.removeAtLocked(idx)
			}
		}
	}
	for _, cmd := range cs.Commands {
		c.ApplyTombstoneCommandUnlocked(cmd)
	}
	c.resortLocked()
	c.maybeCompactLocked()
	c.refreshStatsLocked()
}

func (c *Cache) ApplyTombstoneCommandUnlocked(command string) {
	cmdID, ok := c.pool.ids[command]
	if !ok {
		return
	}
	idx, ok := c.byCmd[cmdID]
	if !ok {
		return
	}
	for id, cid := range c.idToCmd {
		if cid == cmdID {
			delete(c.idToCmd, id)
		}
	}
	c.removeAtLocked(idx)
}

func (c *Cache) MarkDirty() {
	c.mu.Lock()
	c.dirty = true
	c.mu.Unlock()
}

func (c *Cache) Dirty() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.dirty
}

func (c *Cache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st := c.stats
	st.Dirty = c.dirty
	st.Entries = len(c.idToCmd)
	st.UniqueCommands = len(c.recs)
	st.InternedStrings = c.internedCountLocked()
	st.Bytes = c.estimateBytesLocked()
	return st
}

func (c *Cache) Compact() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.compactPoolLocked()
	c.refreshStatsLocked()
}

func (c *Cache) applyCreatedLocked(e Entry) {
	if e.Deleted || e.Command == "" {
		return
	}
	cmdID := c.pool.intern(e.Command)
	c.idToCmd[e.ID] = cmdID
	start := e.StartTS.UnixMilli()
	exit := int32(-1)
	if e.ExitStatus != nil {
		exit = int32(*e.ExitStatus)
	}
	if idx, ok := c.byCmd[cmdID]; ok {
		c.recs[idx].freq++
		if start >= c.recs[idx].lastStart {
			c.recs[idx].lastStart = start
			c.recs[idx].lastExit = exit
			c.recs[idx].cwd = c.pool.intern(e.Cwd)
			c.recs[idx].device = c.pool.intern(e.DeviceID)
			c.recs[idx].session = c.pool.intern(e.SessionID)
		}
		return
	}
	c.byCmd[cmdID] = len(c.recs)
	c.recs = append(c.recs, cacheRec{
		cmd:       cmdID,
		cwd:       c.pool.intern(e.Cwd),
		device:    c.pool.intern(e.DeviceID),
		session:   c.pool.intern(e.SessionID),
		lastStart: start,
		lastExit:  exit,
		freq:      1,
	})
}

func (c *Cache) removeAtLocked(idx int) {
	cmdID := c.recs[idx].cmd
	last := len(c.recs) - 1
	if idx != last {
		c.recs[idx] = c.recs[last]
		c.byCmd[c.recs[idx].cmd] = idx
	}
	c.recs = c.recs[:last]
	delete(c.byCmd, cmdID)
}

func (c *Cache) resortLocked() {
	c.sorted = c.sorted[:0]
	if cap(c.sorted) < len(c.recs) {
		c.sorted = make([]int, 0, len(c.recs))
	}
	for i := range c.recs {
		c.sorted = append(c.sorted, i)
	}
	sort.Slice(c.sorted, func(i, j int) bool {
		return c.pool.get(c.recs[c.sorted[i]].cmd) < c.pool.get(c.recs[c.sorted[j]].cmd)
	})
}

func (c *Cache) prefixEntriesLocked(prefix string) []Entry {
	if len(c.sorted) == 0 {
		return nil
	}
	i := sort.Search(len(c.sorted), func(i int) bool {
		return c.pool.get(c.recs[c.sorted[i]].cmd) >= prefix
	})
	out := make([]Entry, 0, 64)
	for ; i < len(c.sorted); i++ {
		rec := c.recs[c.sorted[i]]
		cmd := c.pool.get(rec.cmd)
		if !strings.HasPrefix(cmd, prefix) {
			break
		}
		if cmd == prefix {
			continue
		}
		out = append(out, c.entryFromLocked(rec))
		if len(out) >= 64 {
			break
		}
	}
	return out
}

func (c *Cache) entryFromLocked(rec cacheRec) Entry {
	e := Entry{
		Command:   c.pool.get(rec.cmd),
		Cwd:       c.pool.get(rec.cwd),
		DeviceID:  c.pool.get(rec.device),
		SessionID: c.pool.get(rec.session),
		StartTS:   time.UnixMilli(rec.lastStart).UTC(),
	}
	if rec.lastExit >= 0 {
		v := int(rec.lastExit)
		e.ExitStatus = &v
	}
	return e
}

func (c *Cache) internedCountLocked() int {
	if c.pool == nil {
		return 0
	}
	return len(c.pool.strs)
}

func (c *Cache) referencedStringCountLocked() int {
	seen := make(map[int32]struct{}, len(c.recs)*4)
	for _, rec := range c.recs {
		seen[rec.cmd] = struct{}{}
		seen[rec.cwd] = struct{}{}
		seen[rec.device] = struct{}{}
		seen[rec.session] = struct{}{}
	}
	return len(seen)
}

func (c *Cache) maybeCompactLocked() {
	if c.pool == nil || len(c.pool.strs) == 0 {
		return
	}
	live := c.referencedStringCountLocked()
	if live == 0 || len(c.pool.strs) > live*2 {
		c.compactPoolLocked()
	}
}

func (c *Cache) compactPoolLocked() {
	if c.pool == nil {
		c.pool = newStringPool()
		return
	}
	next := newStringPool()
	oldToNew := make(map[int32]int32, len(c.pool.strs))
	remap := func(old int32) int32 {
		if id, ok := oldToNew[old]; ok {
			return id
		}
		id := next.intern(c.pool.get(old))
		oldToNew[old] = id
		return id
	}
	newByCmd := make(map[int32]int, len(c.recs))
	for i := range c.recs {
		c.recs[i].cmd = remap(c.recs[i].cmd)
		c.recs[i].cwd = remap(c.recs[i].cwd)
		c.recs[i].device = remap(c.recs[i].device)
		c.recs[i].session = remap(c.recs[i].session)
		newByCmd[c.recs[i].cmd] = i
	}
	newIDToCmd := make(map[string]int32, len(c.idToCmd))
	for id, oldCmd := range c.idToCmd {
		newIDToCmd[id] = remap(oldCmd)
	}
	c.pool = next
	c.byCmd = newByCmd
	c.idToCmd = newIDToCmd
}

func (c *Cache) refreshStatsLocked() {
	c.stats.UniqueCommands = len(c.recs)
	c.stats.Entries = len(c.idToCmd)
	c.stats.InternedStrings = c.internedCountLocked()
	c.stats.Bytes = c.estimateBytesLocked()
}

func (c *Cache) estimateBytesLocked() int64 {
	var n int64
	for _, s := range c.pool.strs {
		n += int64(len(s))
	}
	n += int64(len(c.recs)) * int64(unsafe.Sizeof(cacheRec{}))
	n += int64(len(c.idToCmd)) * 48
	return n
}
