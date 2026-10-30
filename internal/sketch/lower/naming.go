package lower

import (
	"sort"

	"ergo.local/prototype/internal/sketch/check"
)

// The naming pass. Before any body is lowered, it gives every package-scope
// Go entity the program can need its Go name, from the declarations and the
// checker's Info alone: it lowers no body and formats nothing. The lowering
// then names only locals, each in a scope whose parent holds every package
// name, so no local captures a package entity and no package entity a local
// (names.go). A package name the lowering asks for that the pass did not
// give is an internal compiler error (pkgName).
//
// The pass reserves, in this order:
//
//  1. the user package's declarations, in source order: types, functions,
//     lets and wire builders, so a source name keeps its spelling unless Go
//     reserves it;
//  2. the generated package entities: the runtime and time imports, the
//     environment type, the shared empty environment, the pattern test
//     helper and the call table with its entry type (calls.go, #230);
//  3. every type and function of every library package the program reaches
//     (the prelude and imported std packages, by path), used or not;
//  4. the operations adapters of every operations conversion, and the text
//     functions every error type's text needs, in a sorted order.
//
// Reserving an unused library or generated name only renames a local that
// would have taken it; it emits nothing.
//
// Prior art: Gleam's JavaScript backend registers a module's definitions
// before it emits bodies, then allocates locals by counters per scope;
// ReScript prints identifiers once their scope is known.

// nameAll is the naming pass.
func (l *lowerer) nameAll() {
	l.declareNames()
	l.pkgName(pkgKey{"import", runtimeImport}, "ergo")
	l.pkgName(pkgKey{"import", "time"}, "time")
	l.pkgName(pkgKey{"import", taskImport}, "rt")
	l.pkgName(pkgKey{"env", nil}, "Env")
	l.pkgName(pkgKey{"noenv", nil}, "noEnv")
	l.pkgName(pkgKey{"is", nil}, "ergoIs")
	l.pkgName(pkgKey{"call", nil}, "ergoCall")
	l.pkgName(pkgKey{"calls", nil}, "ergoCalls")
	libs := l.libraryPackages()
	for _, p := range libs {
		for _, tn := range p.Types {
			l.typeName(tn)
		}
		for _, f := range p.Funcs {
			l.pkgName(pkgKey{"func", f}, f.Name)
		}
	}
	type entry struct {
		key  pkgKey
		sort string
		base string
	}
	var generated []entry
	for _, p := range append([]*check.Package{l.pkg}, libs...) {
		var convs []check.Conversion
		for _, c := range p.Info.Implicit {
			convs = append(convs, c)
		}
		for _, w := range p.Wires {
			for _, e := range w.Entries {
				convs = append(convs, e.Conversions...)
			}
		}
		for _, c := range convs {
			from, ok := c.From.(*check.Named)
			to, ok2 := c.To.(*check.Named)
			if c.Kind != check.ConvOperations || !ok || !ok2 || mentionsTypeParam(c.From) || mentionsTypeParam(c.To) {
				continue
			}
			key := adapterKey(c)
			generated = append(generated, entry{pkgKey{"adapter", key}, "adapter " + key, "ops" + from.Obj.Name + "As" + to.Obj.Name})
		}
		for _, tn := range p.Types {
			named, ok := tn.Type.(*check.Named)
			if !ok || !(p.ErrorTypes[named] || l.pkg.ErrorTypes[named]) || tn.Decl == nil {
				continue
			}
			l.quiet(func() {
				l.textTypes(named, tn.Decl.Name, map[pkgKey]bool{}, func(t check.Type) {
					key, base := l.textKey(t, tn.Decl.Name)
					generated = append(generated, entry{key, "text " + base + " " + t.String(), base})
				})
			})
		}
	}
	sort.SliceStable(generated, func(i, j int) bool { return generated[i].sort < generated[j].sort })
	for _, e := range generated {
		l.pkgName(e.key, e.base)
	}
	l.prenamed = true
}

// adapterKey identifies the adapter of an operations conversion by its
// source and target types.
func adapterKey(c check.Conversion) string {
	return c.From.String() + " => " + c.To.String()
}

// libraryPackages are the packages other than the user's that the user
// package reaches through its Info, transitively, ordered by path.
func (l *lowerer) libraryPackages() []*check.Package {
	seen := map[*check.Package]bool{l.pkg: true}
	var out []*check.Package
	var add func(p *check.Package)
	add = func(p *check.Package) {
		if p == nil || seen[p] || p.Info == nil {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	visit := func(info *check.Info) {
		for _, o := range info.Uses {
			switch o := o.(type) {
			case *check.PkgName:
				add(o.Pkg)
			case *check.TypeName:
				add(o.Pkg)
			case *check.Func:
				add(o.Pkg)
			}
		}
		for _, t := range info.Types {
			if n, ok := t.(*check.Named); ok {
				add(n.Obj.Pkg)
			}
		}
	}
	visit(l.info)
	for i := 0; i < len(out); i++ {
		visit(out[i].Info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// quiet runs f and drops a refusal: the naming pass meets the same forms
// the lowering refuses later, where the refusal is located and reported.
func (l *lowerer) quiet(f func()) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(refusal); !ok {
				panic(r)
			}
		}
	}()
	f()
}
