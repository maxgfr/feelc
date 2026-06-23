package loader

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/verify"
)

// Cache memoizes candidate compilation by source-content hash. The candidate endpoints
// (/v1/verify, /v1/run, /v1/graph, /v1/check, /v1/trace, /v1/required) all recompile the SAME editor
// buffer repeatedly; caching turns that into one compile + cheap lookups. Compiled models and verify
// reports are immutable after compilation, so sharing a cached value across concurrent requests is safe.
// Bounded with simple FIFO eviction so memory stays capped.
type Cache struct {
	mu    sync.Mutex
	max   int
	items map[string]*cacheEntry
	order []string
	hits  uint64
	calls uint64
}

type cacheEntry struct {
	cm   *ir.CompiledModel
	hash string
	rep  *verify.Report
	err  error
}

// NewCache returns a compile cache holding up to max entries (default 256).
func NewCache(max int) *Cache {
	if max <= 0 {
		max = 256
	}
	return &Cache{max: max, items: make(map[string]*cacheEntry, max)}
}

// Compile returns a cached compilation of src, computing it on a miss. A nil *Cache compiles directly
// (so callers can always go through the cache pointer, even when caching is disabled).
func (c *Cache) Compile(src []byte) (*ir.CompiledModel, string, *verify.Report, error) {
	if c == nil {
		return Compile(src)
	}
	sum := sha256.Sum256(src)
	key := hex.EncodeToString(sum[:])

	c.mu.Lock()
	c.calls++
	if e, ok := c.items[key]; ok {
		c.hits++
		c.mu.Unlock()
		return e.cm, e.hash, e.rep, e.err
	}
	c.mu.Unlock()

	// Compile OUTSIDE the lock so concurrent distinct sources don't serialize. A duplicate concurrent
	// miss just recomputes once more; the store below is idempotent.
	cm, hash, rep, err := Compile(src)

	c.mu.Lock()
	if _, ok := c.items[key]; !ok {
		if len(c.order) >= c.max {
			oldest := c.order[0]
			c.order = c.order[1:]
			delete(c.items, oldest)
		}
		c.items[key] = &cacheEntry{cm: cm, hash: hash, rep: rep, err: err}
		c.order = append(c.order, key)
	}
	c.mu.Unlock()
	return cm, hash, rep, err
}

// Stats returns (hits, calls) for observability/tests.
func (c *Cache) Stats() (hits, calls uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.calls
}
