// Package project loads a feelc PROJECT: a directory of `.rules` modules plus an optional
// `feelc.project.json` manifest, compiled and LINKED into a single ir.CompiledModel that the
// existing engine (VM, verify, graph, explain) runs UNCHANGED. One project = one merged model
// = one project hash.
//
// Design invariant (cf. docs/adr/0015): linking only ever rewrites NAME strings; it never touches
// source line numbers (which are part of the canonical hash, ADR 0006). A single-module project is
// therefore the IDENTITY transform — its hash equals compiling that one file standalone. Multi-module
// namespacing + cross-module references are layered on in later slices.
package project

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/verify"
)

// ManifestName is the conventional manifest file at a project's root (optional; absent ⇒ auto-discover).
const ManifestName = "feelc.project.json"

// sep is the namespace separator used when qualifying a module's names (`credit__eligibility`).
// It is FEEL-identifier-safe (unlike "."), which is why module/local names may not contain it.
const sep = "__"

// maxModules bounds the number of modules in a project (a DoS backstop; far above any real project).
const maxModules = 10000

// moduleNameRe is the strict allowlist for module names: a leading letter then letters/digits/underscore.
// (A leading-anchored allowlist closes control-character / unicode / homoglyph tricks that a blocklist misses.)
var moduleNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// Manifest is the (optional) feelc.project.json descriptor. JSON (stdlib) keeps the dependency set
// unchanged and is consistent with the repo's existing tests/*.claims.json.
type Manifest struct {
	Name          string      `json:"name,omitempty"`
	Version       string      `json:"version,omitempty"`
	Modules       []ModuleRef `json:"modules,omitempty"` // explicit list (deterministic); empty ⇒ auto-discover *.rules
	Default       string      `json:"default,omitempty"` // default (qualified) decision for bare /v1/evaluate
	Tags          []string    `json:"tags,omitempty"`
	Domains       []string    `json:"domains,omitempty"`
	EffectiveDate string      `json:"effectiveDate,omitempty"` // PLACEHOLDER: parsed, not yet used (as-of eval, later phase)
}

// ModuleRef is a manifest entry pointing at one module's source.
type ModuleRef struct {
	Name string            `json:"name"`           // namespace prefix, e.g. "credit"
	Path string            `json:"path"`           // .rules path, relative to the project dir
	Uses map[string]string `json:"uses,omitempty"` // localAlias -> "module.decision" (cross-module refs; later slice)
}

// Module is one compiled `.rules` unit within a project. The standalone Model/Hash/Report are kept
// (pre-merge) so later slices can verify and cache per module by content hash.
type Module struct {
	Name   string
	Path   string // relative to the project dir
	Source []byte
	Model  *ir.CompiledModel
	Hash   string
	Report *verify.Report
	Uses   map[string]string // localInputAlias -> "module.decision" (cross-module bindings; from the manifest)
}

// Project is a loaded, linked project: the per-module units plus the single merged model the engine runs.
type Project struct {
	Manifest Manifest
	Dir      string
	Modules  []*Module
	Merged   *ir.CompiledModel
	Hash     string
}

// Module returns a module by name.
func (p *Project) Module(name string) (*Module, bool) {
	for _, m := range p.Modules {
		if m.Name == name {
			return m, true
		}
	}
	return nil, false
}

// Blockers totals the per-module verification blockers (used by strict mode).
func (p *Project) Blockers() int {
	n := 0
	for _, m := range p.Modules {
		if m.Report != nil {
			n += m.Report.Blockers()
		}
	}
	return n
}

// Load reads a project from a directory (with or without a manifest) or from a single `.rules` file
// (a degenerate one-module project). It compiles every module and links them into Merged/Hash.
func Load(path string) (*Project, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return loadSingleFile(path)
	}

	dir := path
	man, err := readManifest(dir)
	if err != nil {
		return nil, err
	}
	refs := man.Modules
	if len(refs) == 0 {
		if refs, err = discover(dir); err != nil {
			return nil, err
		}
	}
	mods, err := compileModules(dir, refs)
	if err != nil {
		return nil, err
	}
	if man.Name == "" {
		man.Name = filepath.Base(dir)
	}
	p := &Project{Manifest: man, Dir: dir, Modules: mods}
	if err := p.link(); err != nil {
		return nil, err
	}
	return p, nil
}

// SourceModule is an in-memory module (name + source + optional cross-module bindings), used to build a
// candidate project without touching the filesystem (e.g. POST /v1/project/verify, or an edited module).
type SourceModule struct {
	Name   string            `json:"name"`
	Source string            `json:"source"`
	Uses   map[string]string `json:"uses,omitempty"`
}

// Compile builds and links a project from in-memory module sources (no filesystem). It compiles +
// verifies each module, then links exactly like Load, so candidate verification matches served behaviour.
func Compile(name string, mods []SourceModule) (*Project, error) {
	if len(mods) > maxModules {
		return nil, fmt.Errorf("too many modules (%d > %d)", len(mods), maxModules)
	}
	compiled := make([]*Module, 0, len(mods))
	seen := make(map[string]bool, len(mods))
	for _, sm := range mods {
		if err := validateModuleName(sm.Name); err != nil {
			return nil, err
		}
		if seen[sm.Name] {
			return nil, fmt.Errorf("duplicate module name %q", sm.Name)
		}
		seen[sm.Name] = true
		cm, hash, rep, err := loader.CompileFile(sm.Name+".rules", []byte(sm.Source))
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, &Module{
			Name: sm.Name, Path: sm.Name + ".rules", Source: []byte(sm.Source),
			Model: cm, Hash: hash, Report: rep, Uses: sm.Uses,
		})
	}
	sort.Slice(compiled, func(i, j int) bool { return compiled[i].Name < compiled[j].Name })
	if name == "" {
		name = "project"
	}
	p := &Project{Manifest: Manifest{Name: name}, Modules: compiled}
	if err := p.link(); err != nil {
		return nil, err
	}
	return p, nil
}

// loadSingleFile builds a one-module project out of a lone `.rules` file. The merge is the identity
// transform, so the project hash equals loader.Compile(src)'s hash — the back-compat anchor.
func loadSingleFile(path string) (*Project, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cm, hash, rep, err := loader.CompileFile(path, src)
	if err != nil {
		return nil, err
	}
	mod := &Module{Name: moduleNameFromFile(path), Path: filepath.Base(path), Source: src, Model: cm, Hash: hash, Report: rep}
	p := &Project{
		Manifest: Manifest{Name: cm.Name, Modules: []ModuleRef{{Name: mod.Name, Path: mod.Path}}},
		Dir:      filepath.Dir(path),
		Modules:  []*Module{mod},
	}
	if err := p.link(); err != nil {
		return nil, err
	}
	return p, nil
}

// link merges the compiled modules into a single executable model.
//
// Single module ⇒ IDENTITY (the merged model IS the module's model, so the hash is preserved exactly —
// the back-compat anchor). Multiple modules ⇒ namespaced merge (`module__name`), one deterministic hash.
func (p *Project) link() error {
	switch len(p.Modules) {
	case 0:
		return fmt.Errorf("project %q has no modules", p.Manifest.Name)
	case 1:
		m := p.Modules[0]
		if len(m.Uses) > 0 {
			return fmt.Errorf("module %q declares cross-module `uses` but the project has a single module", m.Name)
		}
		p.Merged = m.Model
		p.Hash = m.Hash
		return nil
	default:
		plan, err := buildLinkPlan(p.Modules)
		if err != nil {
			return err
		}
		merged, err := merge(p.Manifest.Name, p.Modules, plan)
		if err != nil {
			return err
		}
		h, err := ir.Hash(merged)
		if err != nil {
			return err
		}
		p.Merged = merged
		p.Hash = hex.EncodeToString(h[:])
		return nil
	}
}

// readManifest loads feelc.project.json if present; a missing manifest is not an error (auto-discover).
func readManifest(dir string) (Manifest, error) {
	var man Manifest
	b, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{}, nil
	}
	if err != nil {
		return Manifest{}, err
	}
	if err := json.Unmarshal(b, &man); err != nil {
		return Manifest{}, fmt.Errorf("%s: %w", ManifestName, err)
	}
	for _, r := range man.Modules {
		if err := validateModuleName(r.Name); err != nil {
			return Manifest{}, fmt.Errorf("%s: %w", ManifestName, err)
		}
	}
	return man, nil
}

// discover lists *.rules files in dir as modules named by their file stem (deterministic, sorted).
func discover(dir string) ([]ModuleRef, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var refs []ModuleRef
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".rules") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".rules")
		if err := validateModuleName(name); err != nil {
			return nil, fmt.Errorf("auto-discovered module %q: %w", e.Name(), err)
		}
		refs = append(refs, ModuleRef{Name: name, Path: e.Name()})
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no .rules modules found in %s and no %s manifest", dir, ManifestName)
	}
	if len(refs) > maxModules {
		return nil, fmt.Errorf("too many modules auto-discovered (%d > %d)", len(refs), maxModules)
	}
	return refs, nil
}

// compileModules reads + compiles each referenced module, rejecting duplicates and returning a
// name-sorted slice (so the merge order — and thus the project hash — is independent of manifest order).
func compileModules(dir string, refs []ModuleRef) ([]*Module, error) {
	if len(refs) > maxModules {
		return nil, fmt.Errorf("too many modules (%d > %d)", len(refs), maxModules)
	}
	seen := make(map[string]bool, len(refs))
	mods := make([]*Module, 0, len(refs))
	for _, r := range refs {
		if err := validateModuleName(r.Name); err != nil {
			return nil, err
		}
		if err := validateModulePath(dir, r.Path); err != nil {
			return nil, fmt.Errorf("module %q: %w", r.Name, err)
		}
		if seen[r.Name] {
			return nil, fmt.Errorf("duplicate module name %q", r.Name)
		}
		seen[r.Name] = true
		full := filepath.Join(dir, r.Path)
		src, err := os.ReadFile(full)
		if err != nil {
			return nil, fmt.Errorf("module %q: %w", r.Name, err)
		}
		cm, hash, rep, err := loader.CompileFile(full, src)
		if err != nil {
			return nil, err // already stamped with file:line:col by CompileFile
		}
		mods = append(mods, &Module{Name: r.Name, Path: r.Path, Source: src, Model: cm, Hash: hash, Report: rep, Uses: r.Uses})
	}
	sort.Slice(mods, func(i, j int) bool { return mods[i].Name < mods[j].Name })
	return mods, nil
}

// validateModuleName enforces a strict identifier allowlist and forbids the namespace separator.
func validateModuleName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("empty module name")
	case len(name) > 64:
		return fmt.Errorf("module name %q too long (max 64)", name)
	case strings.Contains(name, sep):
		return fmt.Errorf("module name %q must not contain %q (reserved namespace separator)", name, sep)
	case !moduleNameRe.MatchString(name):
		return fmt.Errorf("module name %q must match [A-Za-z][A-Za-z0-9_]* (letters, digits, underscore)", name)
	}
	return nil
}

// validateModulePath rejects a manifest module path that escapes the project directory (absolute paths,
// `..` traversal, symlink-style tricks). The path must resolve to a file strictly under dir.
func validateModulePath(dir, path string) error {
	if path == "" {
		return fmt.Errorf("empty module path")
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("module path %q must be relative to the project directory", path)
	}
	rel, err := filepath.Rel(dir, filepath.Join(dir, path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("module path %q escapes the project directory", path)
	}
	return nil
}

// moduleNameFromFile derives a safe module name from a `.rules` file path.
func moduleNameFromFile(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".rules")
	if validateModuleName(base) != nil {
		return "main"
	}
	return base
}
