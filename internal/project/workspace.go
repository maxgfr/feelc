package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Workspace is a project rooted at a directory that supports mutation (edit / create / delete modules)
// and hot-reload. Every mutation follows the GOLDEN RULE: validate an in-memory candidate first, and
// only persist to disk + swap if it links (and, in strict mode, has no verification blockers). The
// current project is held behind a mutex; publishing the swapped project to the registry is the caller's
// job (the service owns the registry), so this type stays pure and unit-testable.
type Workspace struct {
	dir         string
	strict      bool
	hasManifest bool
	isDir       bool

	mu    sync.Mutex
	cur   *Project
	cache moduleCache // incremental-reload cache (dir projects): reuse unchanged modules by source hash
}

// OpenWorkspace loads a project directory for mutation/hot-reload.
func OpenWorkspace(dir string, strict bool) (*Workspace, error) {
	p, err := Load(dir)
	if err != nil {
		return nil, err
	}
	if strict && p.Blockers() > 0 {
		return nil, fmt.Errorf("%d verification blocker(s) (strict mode) — project not opened", p.Blockers())
	}
	info, _ := os.Stat(dir)
	_, statErr := os.Stat(filepath.Join(dir, ManifestName))
	w := &Workspace{dir: dir, strict: strict, hasManifest: statErr == nil, isDir: info != nil && info.IsDir(), cur: p}
	if w.isDir {
		w.cache = cacheOf(p.Modules)
	}
	return w, nil
}

// Current returns the current linked project (lock-free for the caller's snapshot).
func (w *Workspace) Current() *Project {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cur
}

// Reload re-reads the project from disk and swaps it in if valid (incremental for dir projects). Used by
// the watcher and the admin reload endpoint. On error the current project is kept. Taking the lock around
// the whole load serializes reloads against mutations, so a watcher reload cannot publish a stale view.
func (w *Workspace) Reload() (*Project, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reloadLocked()
}

// load reads the project from disk, reusing unchanged modules for a directory project (caller holds w.mu,
// which guards w.cache).
func (w *Workspace) load() (*Project, error) {
	if w.isDir {
		p, cache, err := loadCached(w.dir, w.cache)
		if err != nil {
			return nil, err
		}
		w.cache = cache
		return p, nil
	}
	return Load(w.dir) // single-file project: nothing to reuse
}

// PutModule replaces an existing module's source, validating that the whole project still links before
// writing. Returns the new project on success; on failure the on-disk project and current snapshot are
// untouched (golden rule).
func (w *Workspace) PutModule(name, source string) (*Project, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	m, ok := w.cur.Module(name)
	if !ok {
		return nil, fmt.Errorf("no such module %q (use create to add it)", name)
	}
	if err := w.validateCandidate(w.sourcesReplacing(name, source)); err != nil {
		return nil, err
	}
	if err := writeFileAtomic(filepath.Join(w.dir, m.Path), []byte(source)); err != nil {
		return nil, err
	}
	return w.reloadLocked()
}

// CreateModule adds a new module. With a manifest it also appends the module entry; without one the new
// file is picked up by auto-discovery.
func (w *Workspace) CreateModule(name, source string) (*Project, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := validateModuleName(name); err != nil {
		return nil, err
	}
	if _, ok := w.cur.Module(name); ok {
		return nil, fmt.Errorf("module %q already exists", name)
	}
	path := name + ".rules"
	full := filepath.Join(w.dir, path)
	if _, err := os.Stat(full); err == nil {
		return nil, fmt.Errorf("a file %q already exists in the project directory", path)
	}
	cand := append(w.currentSources(), SourceModule{Name: name, Source: source})
	if err := w.validateCandidate(cand); err != nil {
		return nil, err
	}
	// Write the module file FIRST, then the manifest, so a failure can never leave the manifest
	// referencing a nonexistent module (which would make the whole project unloadable). Roll the file
	// back if the manifest write then fails.
	if err := writeFileAtomic(full, []byte(source)); err != nil {
		return nil, err
	}
	if w.hasManifest {
		man := w.cur.toManifest()
		man.Modules = append(man.Modules, ModuleRef{Name: name, Path: path})
		if err := w.saveManifest(man); err != nil {
			_ = os.Remove(full)
			return nil, err
		}
	}
	return w.reloadLocked()
}

// DeleteModule removes a module, rejecting the delete if another module's `uses` binding depends on it.
func (w *Workspace) DeleteModule(name string) (*Project, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	m, ok := w.cur.Module(name)
	if !ok {
		return nil, fmt.Errorf("no such module %q", name)
	}
	if deps := w.dependentsOf(name); len(deps) > 0 {
		return nil, fmt.Errorf("module %q is used by %s (remove the `uses` binding first)", name, strings.Join(deps, ", "))
	}
	var cand []SourceModule
	for _, sm := range w.currentSources() {
		if sm.Name != name {
			cand = append(cand, sm)
		}
	}
	if len(cand) == 0 {
		return nil, fmt.Errorf("cannot delete the last module of a project")
	}
	if err := w.validateCandidate(cand); err != nil {
		return nil, err
	}
	if w.hasManifest {
		man := w.cur.toManifest()
		kept := man.Modules[:0]
		for _, r := range man.Modules {
			if r.Name != name {
				kept = append(kept, r)
			}
		}
		man.Modules = kept
		if err := w.saveManifest(man); err != nil {
			return nil, err
		}
	}
	if err := os.Remove(filepath.Join(w.dir, m.Path)); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return w.reloadLocked()
}

// Watch hot-reloads the project on any .rules / manifest change in the project directory (fsnotify +
// 150ms debounce, mirroring loader.Watch). onReload is called with the swapped project (or the error).
func (w *Workspace) Watch(onReload func(*Project, error)) (func() error, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(w.dir)
	if err != nil {
		_ = watcher.Close()
		return nil, err
	}
	if err := watcher.Add(abs); err != nil {
		_ = watcher.Close()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		var timer *time.Timer
		debounce := func() {
			p, err := w.Reload()
			if onReload != nil {
				onReload(p, err)
			}
		}
		for {
			select {
			case <-done:
				return
			case ev, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !relevantPath(ev.Name) {
					continue
				}
				if timer != nil {
					timer.Stop()
				}
				timer = time.AfterFunc(150*time.Millisecond, debounce)
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return func() error { close(done); return watcher.Close() }, nil
}

// validateCandidate compiles+links an in-memory candidate and enforces strict mode, WITHOUT persisting.
// It reuses the workspace's compile cache, so validating an edit only recompiles the changed module.
func (w *Workspace) validateCandidate(srcs []SourceModule) error {
	cand, err := compileSourceModules(w.cur.Manifest.Name, srcs, w.cache)
	if err != nil {
		return err
	}
	if w.strict && cand.Blockers() > 0 {
		return fmt.Errorf("%d verification blocker(s) (strict mode) — change not applied", cand.Blockers())
	}
	return nil
}

// reloadLocked reloads from disk (incrementally for dir projects) and swaps cur (caller holds w.mu). It
// re-applies strict mode so no swap path can bypass it.
func (w *Workspace) reloadLocked() (*Project, error) {
	p, err := w.load()
	if err != nil {
		return nil, err
	}
	if w.strict && p.Blockers() > 0 {
		return nil, fmt.Errorf("%d verification blocker(s) (strict mode) — change not applied", p.Blockers())
	}
	w.cur = p
	return p, nil
}

// currentSources snapshots the current modules as in-memory SourceModules (name, source, uses).
func (w *Workspace) currentSources() []SourceModule {
	out := make([]SourceModule, 0, len(w.cur.Modules))
	for _, m := range w.cur.Modules {
		out = append(out, SourceModule{Name: m.Name, Source: string(m.Source), Uses: m.Uses})
	}
	return out
}

// sourcesReplacing returns the current sources with one module's source replaced.
func (w *Workspace) sourcesReplacing(name, source string) []SourceModule {
	out := w.currentSources()
	for i := range out {
		if out[i].Name == name {
			out[i].Source = source
		}
	}
	return out
}

// dependentsOf returns the names of modules that bind (via `uses`) to a decision of the named module.
func (w *Workspace) dependentsOf(name string) []string {
	var deps []string
	for _, m := range w.cur.Modules {
		for _, target := range m.Uses {
			if tmod, _, err := splitRef(target); err == nil && tmod == name {
				deps = append(deps, m.Name)
				break
			}
		}
	}
	sort.Strings(deps)
	return deps
}

// saveManifest atomically writes the manifest to feelc.project.json.
func (w *Workspace) saveManifest(man Manifest) error {
	b, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(w.dir, ManifestName), append(b, '\n'))
}

// toManifest reconstructs a full manifest from the current project (its modules + scalar fields), so a
// create/delete can rewrite feelc.project.json without losing existing entries or bindings.
func (p *Project) toManifest() Manifest {
	man := p.Manifest
	man.Modules = make([]ModuleRef, 0, len(p.Modules))
	for _, m := range p.Modules {
		man.Modules = append(man.Modules, ModuleRef{Name: m.Name, Path: m.Path, Uses: m.Uses})
	}
	return man
}

// writeFileAtomic writes data to path via a temp file + rename in the same directory (no torn writes).
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".feelc-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// relevantPath reports whether a changed path is a module or manifest file (ignore temp/rename churn).
func relevantPath(name string) bool {
	base := filepath.Base(name)
	if strings.HasPrefix(base, ".feelc-") {
		return false // our own atomic-write temp files
	}
	return strings.HasSuffix(name, ".rules") || base == ManifestName
}
