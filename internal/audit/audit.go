// Package audit journalise chaque décision sous forme de ligne JSON rejouable
// (entrée + version/hash du modèle + sortie + durée). Sink io.Writer pluggable.
package audit

import (
	"encoding/json"
	"io"
	"sync"
)

// Record : la trace d'une décision (rejouable).
type Record struct {
	Decision     string         `json:"decision"`
	Input        map[string]any `json:"input"`
	Output       any            `json:"output,omitempty"`
	ModelVersion int64          `json:"modelVersion"`
	Hash         string         `json:"hash"`
	DurationNs   int64          `json:"durationNs"`
	TraceID      string         `json:"traceId,omitempty"`
	Error        string         `json:"error,omitempty"`
}

// Logger écrit des Record en JSON-lines de façon thread-safe.
type Logger struct {
	mu sync.Mutex
	w  io.Writer
}

func New(w io.Writer) *Logger { return &Logger{w: w} }

// Log écrit un enregistrement (best-effort ; les erreurs d'écriture sont ignorées).
func (l *Logger) Log(r Record) {
	if l == nil || l.w == nil {
		return
	}
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	l.mu.Lock()
	l.w.Write(append(b, '\n'))
	l.mu.Unlock()
}
