// Package registry holds the current compiled model and allows an ATOMIC, lock-free
// swap (atomic.Pointer). In-flight requests complete on their snapshot (no tearing);
// a bounded history enables rollback.
package registry

import (
	"sync"
	"sync/atomic"

	"github.com/maxgfr/feelc/internal/ir"
)

const maxHistory = 8

// Entry: a stamped compiled model (monotonic version + content hash for audit/repro).
type Entry struct {
	Model   *ir.CompiledModel
	Version int64
	Hash    string
	Source  []byte // .rules source the model came from (for GET /v1/source; nil if unknown)
}

// Registry: thread-safe holder of the current model.
type Registry struct {
	cur     atomic.Pointer[Entry]
	ver     atomic.Int64
	mu      sync.Mutex
	history []*Entry
}

func New() *Registry { return &Registry{} }

// Current returns the current model (lock-free read; nil if none loaded).
func (r *Registry) Current() *Entry { return r.cur.Load() }

// Store publishes a new model (O(1) lock-free swap) and adds it to the history.
func (r *Registry) Store(cm *ir.CompiledModel, hash string) *Entry {
	return r.StoreWithSource(cm, hash, nil)
}

// StoreWithSource is Store while also keeping the .rules source (served by GET /v1/source).
func (r *Registry) StoreWithSource(cm *ir.CompiledModel, hash string, src []byte) *Entry {
	e := &Entry{Model: cm, Version: r.ver.Add(1), Hash: hash, Source: src}
	r.cur.Store(e)
	r.mu.Lock()
	r.history = append(r.history, e)
	if len(r.history) > maxHistory {
		r.history = r.history[len(r.history)-maxHistory:]
	}
	r.mu.Unlock()
	return e
}

// Rollback republishes the second-to-last model in the history (new version).
func (r *Registry) Rollback() (*Entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.history) < 2 {
		return nil, false
	}
	prev := r.history[len(r.history)-2]
	e := &Entry{Model: prev.Model, Version: r.ver.Add(1), Hash: prev.Hash, Source: prev.Source}
	r.cur.Store(e)
	r.history = append(r.history, e)
	return e, true
}
