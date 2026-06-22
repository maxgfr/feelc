// Package registry conserve le modèle compilé courant et permet un swap ATOMIQUE et
// lock-free (atomic.Pointer). Les requêtes en vol terminent sur leur snapshot (pas de tearing) ;
// un historique borné permet le rollback.
package registry

import (
	"sync"
	"sync/atomic"

	"github.com/maxgfr/feelc/internal/ir"
)

const maxHistory = 8

// Entry : un modèle compilé estampillé (version monotone + hash de contenu pour l'audit/repro).
type Entry struct {
	Model   *ir.CompiledModel
	Version int64
	Hash    string
	Source  []byte // source .rules à l'origine du modèle (pour GET /v1/source ; nil si inconnue)
}

// Registry : porteur thread-safe du modèle courant.
type Registry struct {
	cur     atomic.Pointer[Entry]
	ver     atomic.Int64
	mu      sync.Mutex
	history []*Entry
}

func New() *Registry { return &Registry{} }

// Current renvoie le modèle courant (lecture lock-free ; nil si aucun chargé).
func (r *Registry) Current() *Entry { return r.cur.Load() }

// Store publie un nouveau modèle (swap O(1) lock-free) et l'ajoute à l'historique.
func (r *Registry) Store(cm *ir.CompiledModel, hash string) *Entry {
	return r.StoreWithSource(cm, hash, nil)
}

// StoreWithSource est Store en conservant aussi la source .rules (servie par GET /v1/source).
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

// Rollback republie l'avant-dernier modèle de l'historique (nouvelle version).
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
