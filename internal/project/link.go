package project

import (
	"fmt"
	"strings"

	"github.com/maxgfr/feelc/internal/ir"
)

// link.go implements the namespaced MERGE: every module is qualified under `module__` and the modules
// are concatenated into a single ir.CompiledModel that the existing VM / verify / graph run UNCHANGED.
//
// Qualification rewrites ONLY name strings — never source line numbers — so it cannot perturb the
// canonical hash of a model beyond the intended renaming (ADR 0006). The name-bearing fields are:
//   - CompiledModel.{Inputs,Domains,InputMeta,Units} map keys   (definitions)
//   - Decision.Name                                             (definition)
//   - Decision.Deps[]                                           (references)
//   - DecisionTable.Inputs[]                                    (references — input/decision columns)
//   - ExprProgram.Vars[] in Decision.Expr and every CellTest.Prog (references, recursing through Sub)
// DecisionTable.Outputs[] are local context-field labels (not references) and are left untouched.

// resolver maps a reference (a Dep / table column / bytecode Var) that appears inside a module to its
// fully-qualified name in the merged model. Local references are self-prefixed; a `uses` alias resolves
// to another module's qualified decision name.
type resolver func(local string) (string, error)

// qualified returns the self-qualified form of a module-local definition name.
func qualified(module, local string) string { return module + sep + local }

// linkPlan precomputes, per module, how to resolve references (self-qualify, or follow a cross-module
// `uses` alias) and which of its inputs are BOUND (wired to another module's decision, hence NOT external
// inputs of the merged model). It also validates every alias up front (declared input + existing target).
type linkPlan struct {
	resolver map[string]resolver        // module name -> reference resolver
	bound    map[string]map[string]bool // module name -> set of bound (aliased) input names
}

// buildLinkPlan validates the manifest `uses` bindings and precomputes the per-module resolvers.
func buildLinkPlan(mods []*Module) (*linkPlan, error) {
	byName := make(map[string]*Module, len(mods))
	for _, m := range mods {
		byName[m.Name] = m
	}
	plan := &linkPlan{
		resolver: make(map[string]resolver, len(mods)),
		bound:    make(map[string]map[string]bool, len(mods)),
	}
	for _, m := range mods {
		aliases := make(map[string]string, len(m.Uses)) // local alias -> qualified target decision
		boundSet := make(map[string]bool, len(m.Uses))
		for alias, target := range m.Uses {
			if _, ok := m.Model.Inputs[alias]; !ok {
				return nil, fmt.Errorf("module %q: `uses` alias %q must be a declared input of the module", m.Name, alias)
			}
			tmod, tdec, err := splitRef(target)
			if err != nil {
				return nil, fmt.Errorf("module %q: alias %q -> %q: %w", m.Name, alias, target, err)
			}
			if tmod == m.Name {
				return nil, fmt.Errorf("module %q: alias %q binds to its own module (%q)", m.Name, alias, target)
			}
			other, ok := byName[tmod]
			if !ok {
				return nil, fmt.Errorf("module %q: alias %q -> unknown module %q", m.Name, alias, tmod)
			}
			if _, ok := other.Model.Decision(tdec); !ok {
				return nil, fmt.Errorf("module %q: alias %q -> module %q has no decision %q", m.Name, alias, tmod, tdec)
			}
			aliases[alias] = qualified(tmod, tdec)
			boundSet[alias] = true
		}
		plan.resolver[m.Name] = func(local string) (string, error) {
			if q, ok := aliases[local]; ok {
				return q, nil
			}
			return qualified(m.Name, local), nil
		}
		plan.bound[m.Name] = boundSet
	}
	return plan, nil
}

// splitRef parses a "module.decision" cross-module reference. The dot lives ONLY in the JSON manifest,
// never in a FEEL cell, so this is safe.
func splitRef(ref string) (module, decision string, err error) {
	i := strings.IndexByte(ref, '.')
	if i <= 0 || i >= len(ref)-1 {
		return "", "", fmt.Errorf("expected \"module.decision\"")
	}
	module, decision = ref[:i], ref[i+1:]
	if strings.ContainsRune(decision, '.') {
		return "", "", fmt.Errorf("expected a single \".\" in \"module.decision\"")
	}
	return module, decision, nil
}

// merge namespaces every module under `module__` and concatenates them into one CompiledModel. Because
// names become unique per module, the merged maps never collide; modules are visited in the caller's
// (name-sorted) order, so the canonical hash is deterministic regardless of manifest ordering. Inputs
// bound by a `uses` alias are omitted (they are satisfied by an upstream decision, not an external input).
func merge(projectName string, mods []*Module, plan *linkPlan) (*ir.CompiledModel, error) {
	merged := &ir.CompiledModel{
		Name:      projectName,
		Inputs:    map[string]ir.Type{},
		Domains:   map[string]ir.Domain{},
		InputMeta: map[string]ir.Meta{},
		Units:     map[string]string{},
	}
	for _, m := range mods {
		res := plan.resolver[m.Name]
		bound := plan.bound[m.Name]
		for name, typ := range m.Model.Inputs {
			if bound[name] {
				continue
			}
			merged.Inputs[qualified(m.Name, name)] = typ
		}
		for name, dom := range m.Model.Domains {
			if bound[name] {
				continue
			}
			merged.Domains[qualified(m.Name, name)] = dom
		}
		for name, meta := range m.Model.InputMeta {
			if bound[name] {
				continue
			}
			merged.InputMeta[qualified(m.Name, name)] = meta
		}
		for name, unit := range m.Model.Units {
			if bound[name] {
				continue
			}
			merged.Units[qualified(m.Name, name)] = unit
		}
		for i := range m.Model.Decisions {
			nd, err := qualifyDecision(m.Name, m.Model.Decisions[i], res)
			if err != nil {
				return nil, err
			}
			merged.Decisions = append(merged.Decisions, nd)
		}
	}
	if err := checkAcyclic(merged); err != nil {
		return nil, err
	}
	return merged, nil
}

// qualifyDecision returns a deep-enough copy of d with its name self-qualified and every reference
// (Deps, table columns, bytecode Vars) resolved. The module's standalone Model is never mutated.
func qualifyDecision(module string, d ir.Decision, res resolver) (ir.Decision, error) {
	nd := d // value copy (Table/Expr pointers are cloned below before any rewrite)
	nd.Name = qualified(module, d.Name)
	if len(d.Deps) > 0 {
		deps := make([]string, len(d.Deps))
		for i, dep := range d.Deps {
			q, err := res(dep)
			if err != nil {
				return ir.Decision{}, fmt.Errorf("decision %q: %w", d.Name, err)
			}
			deps[i] = q
		}
		nd.Deps = deps
	}
	if d.Table != nil {
		t, err := cloneTable(d.Table, res)
		if err != nil {
			return ir.Decision{}, fmt.Errorf("decision %q: %w", d.Name, err)
		}
		nd.Table = t
	}
	if d.Expr != nil {
		p, err := cloneProg(d.Expr, res)
		if err != nil {
			return ir.Decision{}, fmt.Errorf("decision %q: %w", d.Name, err)
		}
		nd.Expr = p
	}
	return nd, nil
}

// cloneTable copies a table, qualifying the input columns and the Var names of any Op=Prog cells.
// Outputs are local context labels and are left as-is; HitPolicy/Agg/Priority/Default carry no names.
func cloneTable(t *ir.DecisionTable, res resolver) (*ir.DecisionTable, error) {
	nt := *t
	ins := make([]string, len(t.Inputs))
	for i, in := range t.Inputs {
		q, err := res(in)
		if err != nil {
			return nil, err
		}
		ins[i] = q
	}
	nt.Inputs = ins
	if len(t.Rules) > 0 {
		rules := make([]ir.Rule, len(t.Rules))
		for i, r := range t.Rules {
			nr := r // copies Outputs, OutputSrc, Line (read-only)
			if len(r.Conds) > 0 {
				conds := make([]ir.CellTest, len(r.Conds))
				for j, c := range r.Conds {
					cc, err := cloneCellTest(c, res)
					if err != nil {
						return nil, err
					}
					conds[j] = cc
				}
				nr.Conds = conds
			}
			rules[i] = nr
		}
		nt.Rules = rules
	}
	return &nt, nil
}

// cloneCellTest copies a cell test, qualifying the Vars of an Op=Prog cell and recursing into Sub
// (OpInSet) tests. A/B comparands, Src and Line carry no references and are copied as-is.
func cloneCellTest(c ir.CellTest, res resolver) (ir.CellTest, error) {
	nc := c
	if c.Prog != nil {
		p, err := cloneProg(c.Prog, res)
		if err != nil {
			return ir.CellTest{}, err
		}
		nc.Prog = p
	}
	if len(c.Sub) > 0 {
		sub := make([]ir.CellTest, len(c.Sub))
		for i, s := range c.Sub {
			cs, err := cloneCellTest(s, res)
			if err != nil {
				return ir.CellTest{}, err
			}
			sub[i] = cs
		}
		nc.Sub = sub
	}
	return nc, nil
}

// cloneProg copies a bytecode program, qualifying its Vars. Code (opcodes + integer args) and Consts
// (literals) carry no references and are shared.
func cloneProg(p *ir.ExprProgram, res resolver) (*ir.ExprProgram, error) {
	np := &ir.ExprProgram{Code: p.Code, Consts: p.Consts, MaxStack: p.MaxStack}
	if len(p.Vars) > 0 {
		vars := make([]string, len(p.Vars))
		for i, v := range p.Vars {
			q, err := res(v)
			if err != nil {
				return nil, err
			}
			vars[i] = q
		}
		np.Vars = vars
	}
	return np, nil
}

// checkAcyclic rejects a cyclic decision dependency in the merged model (a cross-module cycle is the
// only new way to introduce one — each module is already acyclic by construction). DFS with coloring.
func checkAcyclic(cm *ir.CompiledModel) error {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(cm.Decisions))
	index := make(map[string]int, len(cm.Decisions))
	for i := range cm.Decisions {
		index[cm.Decisions[i].Name] = i
	}
	var visit func(name string, stack []string) error
	visit = func(name string, stack []string) error {
		switch color[name] {
		case gray:
			return fmt.Errorf("cyclic decision dependency: %s", strings.Join(append(stack, name), " -> "))
		case black:
			return nil
		}
		color[name] = gray
		if i, ok := index[name]; ok {
			for _, dep := range cm.Decisions[i].Deps {
				if _, isDecision := index[dep]; isDecision {
					if err := visit(dep, append(stack, name)); err != nil {
						return err
					}
				}
			}
		}
		color[name] = black
		return nil
	}
	for i := range cm.Decisions {
		if err := visit(cm.Decisions[i].Name, nil); err != nil {
			return err
		}
	}
	return nil
}
