// Package audit logs each decision as a replayable JSON line
// (input + model version/hash + output + duration). Pluggable io.Writer sink.
package audit

import (
	"encoding/json"
	"io"
	"sync"
)

// Record: the trace of a decision (replayable).
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

// Logger writes Records as JSON lines in a thread-safe way.
type Logger struct {
	mu sync.Mutex
	w  io.Writer
}

func New(w io.Writer) *Logger { return &Logger{w: w} }

// Log writes a record (best-effort; write errors are ignored).
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
