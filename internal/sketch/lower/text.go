package lower

import (
	"strconv"
	"strings"

	"ergo.local/prototype/internal/sketch/check"
	"ergo.local/prototype/internal/sketch/syntax"
)

// The text of an error type's value (types.md, Methods 6: it comes from the
// fields). Error() writes the type's name and then the value's text, which a
// generated function per type writes field by field:
//
//   - a record: {name:text name:text}, its fields in declaration order;
//   - a union: its variant's text;
//   - a slice: [text text];
//   - a string: itself; a bool, an integer, a float or a duration: Go's
//     spelling of the value (runtime/v1 Text*);
//   - an operation value: its type's name in angle brackets; a function
//     value: <func>. Its provider or code is not part of the error's data.
//
// No user method takes part: a 196m String or Error function on a field's
// type cannot change the text, as it could through fmt's %v. A field whose
// type has a type parameter (a generic record's T field) or is a Map is
// refused where the error type is declared: the generic text function
// cannot see the instance's fields, and Go methods cannot be specialised.
// A borrow is never a field. Every local of a generated function comes
// from a namer whose parent is the package scope.
//
// Prior art: Gleam's string.inspect and Elixir's Inspect derive a value's
// text from its structure, not from user methods, unless the user opts in.

// textKey is the package-scope key of the text function of t: one per
// declared type, one per slice type.
func (l *lowerer) textKey(t check.Type, site syntax.Node) (pkgKey, string) {
	switch u := t.(type) {
	case *check.Named:
		td := l.typeDecl(u.Obj, site)
		return pkgKey{"text", td.tn}, "text" + td.name
	case *check.Slice:
		return pkgKey{"text", "slice " + t.String()}, "textList"
	}
	return pkgKey{}, ""
}

// textTypes walks the types whose text functions the text of t needs, in
// a fixed order, calling visit once for each; it refuses, at site, a type
// whose text is not lowered.
func (l *lowerer) textTypes(t check.Type, site syntax.Node, seen map[pkgKey]bool, visit func(check.Type)) {
	switch u := t.(type) {
	case *check.Basic:
		switch {
		case u.Kind == check.String, u.Kind == check.Bool, u.Kind == check.Duration,
			u.Kind >= check.Int && u.Kind <= check.Uintptr, u.Kind == check.Float32, u.Kind == check.Float64:
			return
		}
	case *check.Signature:
		return
	case *check.TypeParam:
		refuse(site, "the text of an error type with a field of type parameter %s is lowered in lower-v1 stage 3 (the generic text function cannot see the instance's fields)", u.Name)
	case *check.Named:
		td := l.typeDecl(u.Obj, site)
		if td.kind == kindOps {
			return
		}
		key, _ := l.textKey(t, site)
		if seen[key] {
			return
		}
		seen[key] = true
		visit(t)
		switch td.kind {
		case kindRecord:
			for _, f := range td.fields {
				l.textTypes(f.Type, site, seen, visit)
			}
		case kindUnion:
			if len(td.tparams) > 0 {
				refuse(site, "the text of an error type with a field of generic union type %s is lowered in lower-v1 stage 3", t)
			}
			for _, v := range td.variants {
				l.textTypes(v.Type, site, seen, visit)
			}
		case kindDefined:
			// A defined type's text is its base type's.
			l.textTypes(td.defined, site, seen, visit)
		}
		return
	case *check.Slice:
		if mentionsTypeParam(u) {
			refuse(site, "the text of an error type with a field of type %s is lowered in lower-v1 stage 3", t)
		}
		key, _ := l.textKey(t, site)
		if seen[key] {
			return
		}
		seen[key] = true
		visit(t)
		l.textTypes(u.Elem, site, seen, visit)
		return
	}
	refuse(site, "the text of an error type with a field of type %s is lowered in lower-v1 stage 3", t)
}

// errorMethod writes the Error method of the error type td, and queues the
// text functions its text needs.
func (l *lowerer) errorMethod(b *strings.Builder, td *typeDecl) {
	site := syntax.Node(td.tn.Decl.Name)
	l.textTypes(td.named, site, map[pkgKey]bool{}, func(t check.Type) { l.textFunc(t, site) })
	key, _ := l.textKey(td.named, site)
	scope := newNamer(l.names)
	recv := scope.alloc("e")
	b.WriteString("func (" + recv + " " + l.recvType(td) + ") Error() string {\n\treturn " + strconv.Quote(td.tn.Name) + " + " + l.pkgNames[key] + "(" + recv + ")\n}\n\n")
}

// textExpr is the Go expression of val's text, val of type t, whose text
// function textTypes has queued.
func (l *lowerer) textExpr(t check.Type, val string, site syntax.Node) string {
	switch u := t.(type) {
	case *check.Basic:
		switch {
		case u.Kind == check.String:
			return val
		case u.Kind == check.Bool:
			return l.rt() + ".TextBool(" + val + ")"
		case u.Kind == check.Duration:
			return l.rt() + ".TextDuration(" + val + ")"
		case u.Kind >= check.Int && u.Kind <= check.Int64:
			return l.rt() + ".TextInt(int64(" + val + "))"
		case u.Kind >= check.Uint && u.Kind <= check.Uintptr:
			return l.rt() + ".TextUint(uint64(" + val + "))"
		case u.Kind == check.Float32:
			return l.rt() + ".TextFloat(float64(" + val + "), 32)"
		case u.Kind == check.Float64:
			return l.rt() + ".TextFloat(" + val + ", 64)"
		}
	case *check.Signature:
		return `"<func>"`
	case *check.Named:
		if l.typeDecl(u.Obj, site).kind == kindOps {
			return strconv.Quote("<" + u.Obj.Name + ">")
		}
	}
	key, _ := l.textKey(t, site)
	return l.pkgNames[key] + "(" + val + ")"
}

// textFunc queues the generated text function of t once. Its name was
// given by the naming pass; its type parameters, parameter and locals come
// from its own namer.
func (l *lowerer) textFunc(t check.Type, site syntax.Node) {
	key, base := l.textKey(t, site)
	if l.texts[key] {
		return
	}
	l.texts[key] = true
	name := l.pkgName(key, base)
	scope := newNamer(l.names)
	var head string
	var body func() string
	switch u := t.(type) {
	case *check.Named:
		td := l.useType(l.typeDecl(u.Obj, site))
		list, names := l.typeParams(td.tparams, scope, site)
		param := td.name
		if len(td.tparams) > 0 {
			args := make([]string, len(td.tparams))
			for i, tp := range td.tparams {
				args[i] = names[tp]
			}
			param += "[" + strings.Join(args, ", ") + "]"
		}
		v := scope.alloc("v")
		head = list + "(" + v + " " + param + ")"
		switch td.kind {
		case kindRecord:
			body = func() string {
				var parts []string
				for i, f := range td.fields {
					sep := ""
					if i > 0 {
						sep = " "
					}
					parts = append(parts, strconv.Quote(sep+f.Name+":")+" + "+l.textExpr(f.Type, v+"."+td.fieldName[f.Name], site))
				}
				if len(parts) == 0 {
					return "\treturn \"{}\"\n"
				}
				return "\treturn \"{\" + " + strings.Join(parts, " + ") + " + \"}\"\n"
			}
		case kindUnion:
			body = func() string {
				x := scope.alloc("x")
				var s strings.Builder
				s.WriteString("\tswitch " + x + " := " + v + ".(type) {\n")
				for _, vt := range td.variants {
					vd := l.useType(l.typeDecl(vt, site))
					s.WriteString("\tcase " + vd.name + ":\n\t\treturn " + l.textExpr(vt.Type, x, site) + "\n")
				}
				s.WriteString("\t}\n\treturn \"<nil>\"\n")
				return s.String()
			}
		case kindDefined:
			body = func() string {
				base := (&typeCtx{l: l, params: names, site: site}).goType(td.defined)
				return "\treturn " + l.textExpr(td.defined, base+"("+v+")", site) + "\n"
			}
		}
	case *check.Slice:
		goType := (&typeCtx{l: l, params: map[*check.TypeParam]string{}, site: site}).goType(t)
		v := scope.alloc("v")
		head = "(" + v + " " + goType + ")"
		body = func() string {
			texts, i, e := scope.alloc("texts"), scope.alloc("i"), scope.alloc("e")
			return "\t" + texts + " := make([]string, len(" + v + "))\n\tfor " + i + ", " + e + " := range " + v + " {\n\t\t" + texts + "[" + i + "] = " + l.textExpr(u.Elem, e, site) + "\n\t}\n\treturn " + l.rt() + ".TextList(" + texts + ")\n"
		}
	}
	l.pending = append(l.pending, func() {
		l.gen.WriteString("func " + name + head + " string {\n" + body() + "}\n\n")
	})
}
