// Package loader reads .rules sources, applies the COMPILE-VERIFY-THEN-SWAP pipeline,
// and watches the file (fsnotify + debounce) for hot-reload.
//
// GOLDEN RULE: we NEVER publish an invalid model. A source that does not compile (or, in
// strict mode, that has verification blockers) leaves the service on the previous healthy model.
package loader

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/registry"
	"github.com/maxgfr/feelc/internal/verify"
)

// Compile parses + compiles + verifies a source (without a file name).
func Compile(src []byte) (*ir.CompiledModel, string, *verify.Report, error) {
	return CompileFile("", src)
}

// CompileFile is Compile with a file name propagated onto structured errors
// (diag.Error.File), for usable "file:line:col: ..." diagnostics.
func CompileFile(path string, src []byte) (*ir.CompiledModel, string, *verify.Report, error) {
	m, err := dsl.ParseFile(path, string(src))
	if err != nil {
		return nil, "", nil, err // already stamped by ParseFile
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		return nil, "", nil, diag.WithFileIfDiag(err, path)
	}
	rep := verify.Verify(cm)
	// Hash = CANONICAL identity of the compiled model (hex(ir.Hash)), not of the source text:
	// two sources that compile to the same IR share the hash (intended breaking change, ADR 0006).
	h, err := ir.Hash(cm)
	if err != nil {
		return nil, "", nil, err
	}
	return cm, hex.EncodeToString(h[:]), rep, nil
}

// Reload reads a file and publishes it into reg IF valid. In strict mode, verification
// blockers prevent publication. On error, the current model is kept.
func Reload(path string, reg *registry.Registry, strict bool) (*registry.Entry, *verify.Report, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	cm, hash, rep, err := CompileFile(path, src)
	if err != nil {
		return nil, nil, err // compilation error -> no swap
	}
	if strict && rep.Blockers() > 0 {
		return nil, rep, fmt.Errorf("%d verification blocker(s) (strict mode) — model not published", rep.Blockers())
	}
	return reg.StoreWithSource(cm, hash, src), rep, nil
}

// Watch watches the file (via its directory, to survive editor write-rename operations)
// and triggers Reload after a debounce. Returns a stop function.
func Watch(path string, reg *registry.Registry, strict bool, onReload func(*registry.Entry, *verify.Report, error)) (func() error, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		_ = w.Close()
		return nil, err
	}
	if err := w.Add(filepath.Dir(abs)); err != nil {
		_ = w.Close()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		var timer *time.Timer
		debounce := func() {
			e, rep, err := Reload(path, reg, strict)
			if onReload != nil {
				onReload(e, rep, err)
			}
		}
		for {
			select {
			case <-done:
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if evAbs, _ := filepath.Abs(ev.Name); evAbs != abs {
					continue
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(150*time.Millisecond, debounce)
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return func() error {
		close(done)
		return w.Close()
	}, nil
}
