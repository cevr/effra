// Package migrate196 is the one-off translator from the Go-shaped syntax of
// 92fac236 to the decided syntax of #196 (D5). It parses old syntax with a
// frozen copy of that parser (oldsyntax), rewrites the source by edits the
// old tree locates, and prints the result with internal/sketch/format, so
// its output is canonical and keeps every comment. A form with no decided
// spelling of the same meaning is left as it was and reported as a note for
// a hand edit; the translator never emits a form whose meaning differs.
// It is deleted after v1 (#196).
package migrate196

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"ergo.local/prototype/internal/migrate196/oldsyntax"
	"ergo.local/prototype/internal/sketch/format"
	"ergo.local/prototype/internal/sketch/syntax"
)

// Note is a place the translator left for a hand edit.
type Note struct {
	Line int
	Text string
}

func (n Note) String() string { return fmt.Sprintf("%d: %s", n.Line, n.Text) }

// Translate rewrites one old-syntax source into the decided syntax. The
// error is non-nil when src does not parse as old syntax, or when the
// rewritten text does not parse (a translator bug, reported with the
// text). With notes the output still holds the noted old forms and may not
// parse; then out is the edited text before formatting.
func Translate(src string) (out string, notes []Note, err error) {
	file, err := oldsyntax.Parse(src)
	if err != nil {
		return "", nil, err
	}
	t := &translator{src: src, file: file, parent: map[oldsyntax.Node]oldsyntax.Node{}}
	t.run()
	text := t.apply()
	sort.SliceStable(t.notes, func(i, j int) bool { return t.notes[i].Line < t.notes[j].Line })
	formatted, ferr := format.Source(text)
	if ferr != nil {
		if len(t.notes) > 0 {
			return text, t.notes, nil
		}
		return text, nil, fmt.Errorf("translation does not parse: %v", ferr)
	}
	return formatted, t.notes, nil
}

type edit struct {
	start, end int
	text       string
	seq        int
}

type translator struct {
	src    string
	file   *oldsyntax.File
	edits  []edit
	notes  []Note
	parent map[oldsyntax.Node]oldsyntax.Node

	// methods are the names declared with a receiver; types the type names
	// declared in the file.
	methods map[string]bool
	types   map[string]bool
	ops     map[string]bool // operation names of the file's interfaces
	fields  map[string]bool // field names of the file's structs
}

// ---------------------------------------------------------------- edits and notes

func (t *translator) replace(start, end oldsyntax.Pos, text string) {
	t.edits = append(t.edits, edit{int(start), int(end), text, len(t.edits)})
}

func (t *translator) insert(at oldsyntax.Pos, text string) { t.replace(at, at, text) }

func (t *translator) note(pos oldsyntax.Pos, format string, args ...any) {
	t.notes = append(t.notes, Note{Line: t.file.Line(pos), Text: fmt.Sprintf(format, args...)})
}

func (t *translator) text(n oldsyntax.Node) string { return t.src[n.Pos():n.End()] }

// apply performs the edits from the end of the source backwards. Edits that
// overlap are a translator bug: the later one becomes a note.
func (t *translator) apply() string {
	sort.SliceStable(t.edits, func(i, j int) bool {
		a, b := t.edits[i], t.edits[j]
		if a.start != b.start {
			return a.start < b.start
		}
		if ai, bi := a.start == a.end, b.start == b.end; ai != bi {
			return ai
		}
		return a.seq < b.seq
	})
	var b strings.Builder
	last := 0
	for _, e := range t.edits {
		if e.start < last {
			t.note(oldsyntax.Pos(e.start), "internal: overlapping rewrite skipped")
			continue
		}
		b.WriteString(t.src[last:e.start])
		b.WriteString(e.text)
		last = e.end
	}
	b.WriteString(t.src[last:])
	return b.String()
}

// ---------------------------------------------------------------- the walk

func (t *translator) run() {
	t.methods, t.types, t.ops, t.fields = map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	var stack []oldsyntax.Node
	oldsyntax.Inspect(t.file, func(n oldsyntax.Node) bool {
		for len(stack) > 0 {
			top := stack[len(stack)-1]
			if top.Pos() <= n.Pos() && n.End() <= top.End() {
				break
			}
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			t.parent[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	for _, d := range t.file.Decls {
		switch d := d.(type) {
		case *oldsyntax.FuncDecl:
			if d.Recv != nil {
				t.methods[d.Name.Name] = true
			}
		case *oldsyntax.TypeDecl:
			t.types[d.Name.Name] = true
			if st, ok := d.Spec.(*oldsyntax.StructType); ok {
				for _, f := range st.Fields.List {
					for _, n := range f.Names {
						t.fields[n.Name] = true
					}
				}
			}
			if it, ok := d.Spec.(*oldsyntax.InterfaceType); ok {
				for _, m := range it.Methods {
					if len(m.Names) > 0 {
						t.ops[m.Names[0].Name] = true
					}
				}
			}
		}
	}
	t.renameKeywords()
	if t.file.Name != nil {
		end := t.file.Name.End()
		for int(end) < len(t.src) && (t.src[end] == ' ' || t.src[end] == '\t') {
			end++
		}
		if int(end) < len(t.src) && t.src[end] == '\n' {
			end++
		}
		t.replace(t.file.Package, end, "")
	}
	for _, d := range t.file.Decls {
		t.decl(d)
	}
	oldsyntax.Inspect(t.file, func(n oldsyntax.Node) bool {
		t.node(n)
		return true
	})
}

func (t *translator) decl(d oldsyntax.Decl) {
	switch d := d.(type) {
	case *oldsyntax.ImportDecl:
		t.importDecl(d)
	case *oldsyntax.VarDecl:
		t.varDecl(d, true)
	case *oldsyntax.TypeDecl:
		t.typeDecl(d)
	case *oldsyntax.FuncDecl:
		t.funcDecl(d)
	case *oldsyntax.WireDecl:
		t.replace(d.Wire, d.Name.Pos(), "let ")
		t.insert(d.Name.End(), " = wire")
		t.wireBody(d.Body)
	}
}

func (t *translator) importDecl(d *oldsyntax.ImportDecl) {
	if d.Lparen.IsValid() {
		t.replace(d.Import, d.Lparen+1, "")
		t.replace(d.Rparen, d.Rparen+1, "")
		for _, s := range d.Specs {
			t.insert(s.Pos(), "import ")
			t.importSpec(s)
		}
		return
	}
	t.importSpec(d.Specs[0])
}

func (t *translator) importSpec(s *oldsyntax.ImportSpec) {
	path, err := strconv.Unquote(s.Path.Value)
	if err != nil {
		t.note(s.Path.Pos(), "import path %s is not a string", s.Path.Value)
		return
	}
	switch s.Kind {
	case oldsyntax.ImportDot:
		t.note(s.Pos(), "dot import of %s: Ergo imports are qualified", path)
		return
	case oldsyntax.ImportNamed:
		if last := path[strings.LastIndexByte(path, '/')+1:]; last == s.Name.Name {
			t.replace(s.Name.Pos(), s.Path.Pos(), "")
		}
	}
	t.replace(s.Path.Pos(), s.Path.End(), path)
}

// ---------------------------------------------------------------- declarations

func (t *translator) varDecl(d *oldsyntax.VarDecl, top bool) {
	if d.Lparen.IsValid() {
		t.replace(d.Var, d.Lparen+1, "")
		t.replace(d.Rparen, d.Rparen+1, "")
		for _, s := range d.Specs {
			t.insert(s.Pos(), t.letWord(s.Names, top, s))
		}
		return
	}
	s := d.Specs[0]
	t.replace(d.Var, s.Pos(), t.letWord(s.Names, top, s))
	if len(s.Values) > 1 {
		t.note(s.Pos(), "a var with several values: write one let per name")
	}
}

// letWord is `let ` or `let mut ` for names declared in a function: mut
// when any is assigned or has its address taken in the function.
func (t *translator) letWord(names []*oldsyntax.Ident, top bool, at oldsyntax.Node) string {
	for _, n := range names {
		t.keywordName(n)
	}
	if top {
		return "let "
	}
	fn := t.enclosingFunc(at)
	for _, n := range names {
		if n.Name != "_" && fn != nil && t.written(fn, n.Name) {
			return "let mut "
		}
	}
	return "let "
}

// keywordName is kept for the declaration sites; renameKeywords renames
// every name the decided grammar made a keyword.
func (t *translator) keywordName(*oldsyntax.Ident) {}

// renameKeywords renames each name that the decided grammar made a keyword
// (`with`, `in`, `test`, …) to the name and the first free number (`with2`,
// as the prototype refuses a trailing `_`), everywhere in
// the file: a consistent renaming keeps the meaning of a one-file program.
// The with form's own `with` and the `go` qualifier are not names.
func (t *translator) renameKeywords() {
	used := map[string]bool{}
	var hits []*oldsyntax.Ident
	oldsyntax.Inspect(t.file, func(n oldsyntax.Node) bool {
		id, ok := n.(*oldsyntax.Ident)
		if !ok {
			return true
		}
		used[id.Name] = true
		if !syntax.IsKeyword(id.Name) || id == t.file.Name {
			return true
		}
		switch p := t.parent[id].(type) {
		case *oldsyntax.CallExpr:
			if p.Fun == oldsyntax.Expr(id) && id.Name == "with" {
				return true
			}
		case *oldsyntax.SelectorExpr:
			if p.X == oldsyntax.Expr(id) && id.Name == "go" {
				return true
			}
		}
		hits = append(hits, id)
		return true
	})
	renamed := map[string]string{}
	for _, id := range hits {
		name, ok := renamed[id.Name]
		for i := 2; !ok; i++ {
			name = fmt.Sprintf("%s%d", id.Name, i)
			ok = !used[name]
		}
		renamed[id.Name] = name
		t.replace(id.Pos(), id.End(), name)
	}
}

func (t *translator) enclosingFunc(n oldsyntax.Node) oldsyntax.Node {
	for p := t.parent[n]; p != nil; p = t.parent[p] {
		switch p.(type) {
		case *oldsyntax.FuncDecl, *oldsyntax.FuncLit:
			return p
		}
	}
	return nil
}

// written reports whether name is assigned, incremented or has its address
// taken anywhere in fn (a conservative test: any write to the name).
func (t *translator) written(fn oldsyntax.Node, name string) bool {
	found := false
	root := func(x oldsyntax.Expr) bool {
		for {
			switch e := x.(type) {
			case *oldsyntax.Ident:
				return e.Name == name
			case *oldsyntax.SelectorExpr:
				x = e.X
			case *oldsyntax.IndexExpr:
				x = e.X
			case *oldsyntax.ParenExpr:
				x = e.X
			default:
				return false
			}
		}
	}
	oldsyntax.Inspect(fn, func(n oldsyntax.Node) bool {
		switch n := n.(type) {
		case *oldsyntax.AssignStmt:
			if n.Tok != oldsyntax.DEFINE {
				for _, l := range n.Lhs {
					found = found || root(l)
				}
			}
		case *oldsyntax.IncDecStmt:
			found = found || root(n.X)
		case *oldsyntax.UnaryExpr:
			found = found || n.Op == oldsyntax.AND && root(n.X)
		case *oldsyntax.RangeStmt:
			if n.Tok == oldsyntax.ASSIGN {
				found = found || n.Key != nil && root(n.Key) || n.Value != nil && root(n.Value)
			}
		}
		return !found
	})
	return found
}

func (t *translator) typeDecl(d *oldsyntax.TypeDecl) {
	t.typeParams(d.TypeParams)
	if d.TypeParams == nil {
		if implicit := t.implicitParams(d.Spec); len(implicit) > 0 {
			t.insert(d.Name.End(), "["+strings.Join(implicit, ", ")+"]")
		}
	}
	switch s := d.Spec.(type) {
	case *oldsyntax.StructType:
		t.members(s.Struct, s.Fields.Opening, s.Fields.Closing, s.Fields.List)
	case *oldsyntax.InterfaceType:
		if s.Union != nil {
			parts := make([]string, len(s.Union))
			for i, x := range s.Union {
				parts[i] = t.text(x)
			}
			t.replace(s.Interface, s.Rbrace+1, "= "+strings.Join(parts, " | "))
			t.skip(s)
			return
		}
		t.members(s.Interface, s.Lbrace, s.Rbrace, s.Methods)
	case *oldsyntax.MachineType:
		for _, e := range s.Entries {
			t.insert(e.NamePos+oldsyntax.Pos(len(e.Name)), ":")
		}
		t.separate(s.Lbrace, s.Rbrace, nodesOf(s.Entries))
	}
}

// skipped holds nodes whose text a replacement already rewrote whole.
func (t *translator) skip(n oldsyntax.Node) {
	oldsyntax.Inspect(n, func(c oldsyntax.Node) bool {
		t.parent[c] = skipped{}
		return true
	})
}

type skipped struct{ oldsyntax.Node }

func (t *translator) isSkipped(n oldsyntax.Node) bool {
	_, ok := t.parent[n].(skipped)
	return ok
}

// members turns `struct { … }` or `interface { … }` into `{ m, m }`: the
// keyword goes and the members are separated by commas.
func (t *translator) members(keyword, lbrace, rbrace oldsyntax.Pos, fields []*oldsyntax.Field) {
	t.replace(keyword, lbrace, "")
	for _, f := range fields {
		if len(f.Names) == 0 {
			t.note(f.Pos(), "an embedded member: Ergo has no embedding")
		}
	}
	t.separate(lbrace, rbrace, nodesOf(fields))
}

// separate writes a comma between consecutive items of a braced list whose
// old separators are semicolons or line breaks.
func (t *translator) separate(lbrace, rbrace oldsyntax.Pos, items []oldsyntax.Node) {
	for i, it := range items {
		next := rbrace
		if i+1 < len(items) {
			next = items[i+1].Pos()
		}
		gap := t.src[it.End():next]
		if j := strings.IndexByte(stripComments(gap), ';'); j >= 0 {
			t.replace(it.End()+oldsyntax.Pos(j), it.End()+oldsyntax.Pos(j)+1, ",")
		} else if i+1 < len(items) || strings.Contains(gap, "\n") {
			t.insert(it.End(), ",")
		}
	}
}

// stripComments blanks the comments of a gap, keeping offsets.
func stripComments(gap string) string {
	b := []byte(gap)
	for i := 0; i+1 < len(b); i++ {
		if b[i] == '/' && b[i+1] == '/' {
			for i < len(b) && b[i] != '\n' {
				b[i] = ' '
				i++
			}
		} else if b[i] == '/' && b[i+1] == '*' {
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				b[i] = ' '
				i++
			}
		}
	}
	return string(b)
}

func nodesOf[T oldsyntax.Node](list []T) []oldsyntax.Node {
	out := make([]oldsyntax.Node, len(list))
	for i, x := range list {
		out[i] = x
	}
	return out
}

func (t *translator) typeParams(l *oldsyntax.FieldList) {
	if l == nil {
		return
	}
	for _, f := range l.List {
		if id, ok := f.Type.(*oldsyntax.Ident); ok && id.Name == "any" {
			t.replace(f.Names[len(f.Names)-1].End(), id.End(), "")
		}
		if s, ok := f.Type.(*oldsyntax.SelectorExpr); ok && t.text(s) == "cmp.Ordered" {
			t.replace(s.Pos(), s.End(), "ordered")
		}
	}
}

// implicitParams returns the one-letter upper-case names that n uses as
// types without a declaration (the old implicit type parameters), in order.
func (t *translator) implicitParams(n oldsyntax.Node) []string {
	var out []string
	seen := map[string]bool{}
	oldsyntax.Inspect(n, func(c oldsyntax.Node) bool {
		switch c := c.(type) {
		case *oldsyntax.SelectorExpr:
			return false
		case *oldsyntax.Ident:
			if len(c.Name) == 1 && unicode.IsUpper(rune(c.Name[0])) && !t.types[c.Name] && !seen[c.Name] && t.isTypePosition(c) {
				seen[c.Name] = true
				out = append(out, c.Name)
			}
		}
		return true
	})
	return out
}

// isTypePosition reports whether an identifier stands where a type does: a
// field or parameter type, a type argument, an error row element.
func (t *translator) isTypePosition(id *oldsyntax.Ident) bool {
	switch p := t.parent[id].(type) {
	case *oldsyntax.Field:
		return p.Type == oldsyntax.Expr(id)
	case *oldsyntax.IndexExpr, *oldsyntax.ArrayType, *oldsyntax.StarExpr, *oldsyntax.MapType, *oldsyntax.ErrorRow, *oldsyntax.ChanType, *oldsyntax.TupleExpr, *oldsyntax.Ellipsis, *oldsyntax.NeedsClause:
		return true
	}
	return false
}

func (t *translator) funcDecl(d *oldsyntax.FuncDecl) {
	t.keywordName(d.Name)
	if d.Body == nil {
		t.externDecl(d)
	}
	if strings.HasPrefix(d.Name.Name, "Test") && t.testDecl(d) {
		return
	}
	t.typeParams(d.TypeParams)
	var implicit []string
	if d.TypeParams == nil {
		implicit = t.implicitParams(d.Type)
		if d.Recv != nil {
			implicit = append(t.implicitParams(d.Recv), implicit...)
		}
		implicit = unique(implicit)
	}
	if d.Recv != nil {
		r := d.Recv.List[0]
		recv := t.text(r.Type)
		if len(r.Names) > 0 {
			recv = r.Names[0].Name + " " + recv
		} else {
			recv = "_ " + recv
		}
		t.replace(d.Recv.Opening, d.Name.Pos(), "")
		sep := ", "
		if len(d.Type.Params.List) == 0 {
			sep = ""
		}
		t.insert(d.Type.Params.Opening+1, recv+sep)
	}
	if len(implicit) > 0 {
		t.insert(d.Name.End(), "["+strings.Join(implicit, ", ")+"]")
	}
}

func unique(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// externDecl turns `//ef:go pkg.F` and a bodiless function into `extern go
// pkg.F func …`.
func (t *translator) externDecl(d *oldsyntax.FuncDecl) {
	var directive *oldsyntax.Comment
	for _, g := range t.file.Comments {
		for _, c := range g.List {
			if strings.HasPrefix(c.Text, "//ef:go ") && c.EndPos <= d.Func && t.file.Line(c.Slash) >= t.file.Line(d.Func)-1 {
				directive = c
			}
		}
	}
	if directive == nil {
		t.note(d.Pos(), "a function without a body: give it one, or bind it with extern go")
		return
	}
	target := strings.TrimSpace(strings.TrimPrefix(directive.Text, "//ef:go "))
	end := directive.EndPos
	if int(end) < len(t.src) && t.src[end] == '\n' {
		end++
	}
	t.replace(directive.Slash, end, "")
	t.insert(d.Func, "extern go "+target+" ")
}

// testDecl turns `func TestX() needs testing.T { … }` into `test X { … }`
// when every use of T is T.Equal(a, b) or T.True(c).
func (t *translator) testDecl(d *oldsyntax.FuncDecl) bool {
	ty := d.Type
	if d.Recv != nil || d.TypeParams != nil || len(ty.Params.List) != 0 || ty.Needs == nil || len(ty.Needs.Names) != 1 || t.text(ty.Needs.Names[0]) != "testing.T" {
		return false
	}
	ok := true
	var edits []edit
	oldsyntax.Inspect(d.Body, func(n oldsyntax.Node) bool {
		switch n := n.(type) {
		case *oldsyntax.CallExpr:
			sel, isSel := n.Fun.(*oldsyntax.SelectorExpr)
			if !isSel || t.text(sel.X) != "T" {
				return true
			}
			switch {
			case sel.Sel.Name == "Equal" && len(n.Args) == 2:
				edits = append(edits, edit{int(n.Pos()), int(n.Args[0].Pos()), "assert ", 0}, edit{int(n.Args[0].End()), int(n.Args[1].Pos()), " == ", 0}, edit{int(n.Rparen), int(n.Rparen) + 1, "", 0})
			case sel.Sel.Name == "True" && len(n.Args) == 1:
				edits = append(edits, edit{int(n.Pos()), int(n.Args[0].Pos()), "assert ", 0}, edit{int(n.Rparen), int(n.Rparen) + 1, "", 0})
			default:
				ok = false
			}
			return false
		case *oldsyntax.Ident:
			if n.Name == "T" {
				ok = false
			}
		}
		return true
	})
	if !ok {
		t.note(d.Pos(), "test %s uses testing.T beyond T.Equal and T.True: rewrite it with assert", d.Name.Name)
		return false
	}
	if ty.Results != nil {
		// A test with only a failure row: a failure that escapes the body
		// fails the test, in Go (a returned error) and in Ergo alike.
		if _, row := ty.Results.List[0].Type.(*oldsyntax.ErrorRow); !row || len(ty.Results.List) != 1 {
			t.note(d.Pos(), "test %s has a result: a test fails through assert", d.Name.Name)
			return false
		}
	}
	t.replace(d.Func, d.Name.Pos(), "test ")
	t.replace(d.Name.Pos(), d.Name.End(), strings.TrimPrefix(d.Name.Name, "Test"))
	t.replace(ty.Params.Opening, ty.Needs.End(), "")
	t.skip(ty)
	for _, e := range edits {
		t.replace(oldsyntax.Pos(e.start), oldsyntax.Pos(e.end), e.text)
	}
	return true
}

// ---------------------------------------------------------------- nodes

func (t *translator) node(n oldsyntax.Node) {
	if t.isSkipped(n) {
		return
	}
	switch n := n.(type) {
	case *oldsyntax.Field:
		for _, id := range n.Names {
			t.keywordName(id)
		}
	case *oldsyntax.FuncType:
		t.results(n)
	case *oldsyntax.MapType:
		t.replace(n.Map, n.Key.Pos(), "Map[")
		t.replace(n.Key.End(), n.Value.Pos(), ", ")
		t.insert(n.Value.End(), "]")
	case *oldsyntax.StructType:
		if _, isDecl := t.parent[n].(*oldsyntax.TypeDecl); !isDecl {
			t.note(n.Pos(), "an anonymous struct: declare a named record type")
		}
	case *oldsyntax.InterfaceType:
		if _, isDecl := t.parent[n].(*oldsyntax.TypeDecl); !isDecl {
			t.note(n.Pos(), "an anonymous interface: declare a named type")
		}
	case *oldsyntax.ChanType:
		t.note(n.Pos(), "a channel: Ergo has no channels; use a typed queue")
	case *oldsyntax.ArrayType:
		if n.Len != nil {
			t.note(n.Pos(), "a fixed array: use a slice")
		}
	case *oldsyntax.Ellipsis:
		t.note(n.Pos(), "a variadic parameter: take a slice")
	case *oldsyntax.SelectStmt:
		t.note(n.Pos(), "select: race tasks with task.Race")
	case *oldsyntax.SendStmt:
		t.note(n.Pos(), "a channel send: use a typed queue")
	case *oldsyntax.LabeledStmt:
		t.note(n.Pos(), "a label: Ergo has no labels")
	case *oldsyntax.BranchStmt:
		t.branch(n)
	case *oldsyntax.UnaryExpr:
		if n.Op == oldsyntax.ARROW {
			t.note(n.Pos(), "a channel receive: use a typed queue")
		}
		t.tryOperands(n)
	case *oldsyntax.Ident:
		if n.Name == "any" && t.isTypePosition(n) {
			t.note(n.Pos(), "any as a type: name the type")
		}
	case *oldsyntax.SelectorExpr:
		t.methodValue(n)
	case *oldsyntax.CallExpr:
		t.call(n)
	case *oldsyntax.WireLit:
		t.wireBody(n.Body)
	case *oldsyntax.BinaryExpr:
		t.tryOperands(n)
	case *oldsyntax.CatchExpr:
		t.catch(n)
	case *oldsyntax.GoExpr:
		if n.Block != nil {
			t.replace(n.Go, n.Block.Pos(), "task.Go() ")
		} else {
			t.replace(n.Go, n.Call.Pos(), "task.Go() { ")
			t.insert(n.Call.End(), " }")
		}
	case *oldsyntax.DeferStmt:
		t.replace(n.Defer, n.Stmt.Pos(), "scope.OnExit() { ")
		t.insert(n.Stmt.End(), " }")
	case *oldsyntax.DeclStmt:
		t.varDecl(n.Decl, false)
	case *oldsyntax.AssignStmt:
		t.assign(n)
	case *oldsyntax.IncDecStmt:
		if _, isFor := t.parent[n].(*oldsyntax.ForStmt); !isFor {
			t.replace(n.TokPos, n.TokPos+2, map[oldsyntax.Token]string{oldsyntax.INC: " += 1", oldsyntax.DEC: " -= 1"}[n.Tok])
		}
	case *oldsyntax.ReturnStmt:
		if len(n.Results) > 1 {
			t.insert(n.Results[0].Pos(), "(")
			t.insert(n.Results[len(n.Results)-1].End(), ")")
		}
	case *oldsyntax.IfStmt:
		if n.Init != nil {
			t.hoistInit(n.If, n.Init, n.Cond.Pos())
		}
	case *oldsyntax.ForStmt:
		t.forStmt(n)
	case *oldsyntax.RangeStmt:
		t.rangeStmt(n)
	case *oldsyntax.SwitchStmt:
		t.switchStmt(n)
	case *oldsyntax.TypeSwitchStmt:
		t.typeSwitch(n)
	}
}

// results rewrites `(T, error[E])` to `T fails E` and `error[E]` to
// `fails E`, and notes named results.
func (t *translator) results(ft *oldsyntax.FuncType) {
	r := ft.Results
	if r == nil {
		return
	}
	var row *oldsyntax.ErrorRow
	var types []oldsyntax.Expr
	for _, f := range r.List {
		if len(f.Names) > 0 {
			t.note(f.Pos(), "a named result: return the value")
			return
		}
		if e, ok := f.Type.(*oldsyntax.ErrorRow); ok {
			row = e
			continue
		}
		types = append(types, f.Type)
	}
	if row == nil {
		return
	}
	rowText := t.rowText(row)
	switch {
	case !r.Opening.IsValid():
		t.replace(row.Pos(), row.End(), rowText)
	case len(types) == 0:
		t.replace(r.Opening, r.Closing+1, rowText)
	case len(types) == 1:
		t.replace(r.Opening, types[0].Pos(), "")
		t.replace(types[0].End(), r.Closing+1, " "+rowText)
	default:
		last := types[len(types)-1]
		t.replace(last.End(), r.Closing, ")")
		t.replace(r.Closing, r.Closing+1, " "+rowText)
	}
}

func (t *translator) rowText(row *oldsyntax.ErrorRow) string {
	parts := make([]string, len(row.Types))
	for i, x := range row.Types {
		parts[i] = t.text(x)
	}
	return "fails " + strings.Join(parts, " | ")
}

// methodValue notes a method named without a call: a method value has no
// decided spelling of the same meaning.
func (t *translator) methodValue(s *oldsyntax.SelectorExpr) {
	if !t.methods[s.Sel.Name] || t.fields[s.Sel.Name] {
		return
	}
	if c, ok := t.parent[s].(*oldsyntax.CallExpr); ok && c.Fun == oldsyntax.Expr(s) {
		return
	}
	if id, ok := s.X.(*oldsyntax.Ident); ok && t.types[id.Name] {
		return
	}
	t.note(s.Pos(), "a method value %s: write a function literal", t.text(s))
}

func (t *translator) call(c *oldsyntax.CallExpr) {
	if c.Ellipsis.IsValid() {
		t.note(c.Ellipsis, "a spread argument: pass the slice")
		return
	}
	id, ok := c.Fun.(*oldsyntax.Ident)
	if !ok {
		return
	}
	switch id.Name {
	case "with":
		switch {
		case c.Trailing != nil:
			t.replace(c.Lparen, c.Lparen+1, " ")
			t.replace(c.Rparen, c.Rparen+1, "")
		case len(c.Args) >= 2:
			// with(W, f) runs f under W: `with W { f() }`.
			n := len(c.Args)
			t.replace(c.Lparen, c.Lparen+1, " ")
			t.replace(c.Args[n-2].End(), c.Args[n-1].Pos(), " { ")
			if _, lit := c.Args[n-1].(*oldsyntax.FuncLit); lit {
				t.insert(c.Args[n-1].Pos(), "(")
				t.replace(c.Rparen, c.Rparen+1, ")() }")
			} else {
				t.replace(c.Rparen, c.Rparen+1, "() }")
			}
		default:
			t.note(c.Pos(), "with without a body")
		}
	case "append":
		if len(c.Args) < 2 {
			t.note(c.Pos(), "append without elements")
			return
		}
		t.replace(c.Pos(), c.Args[0].Pos(), "")
		for i := 1; i < len(c.Args); i++ {
			sep := ").Append("
			if i == 1 {
				sep = ".Append("
			}
			t.replace(c.Args[i-1].End(), c.Args[i].Pos(), sep)
		}
	case "delete":
		if len(c.Args) != 2 {
			return
		}
		t.replace(c.Pos(), c.Args[0].Pos(), "")
		t.replace(c.Args[0].End(), c.Args[1].Pos(), ".Delete(")
	case "make", "new", "cap", "copy", "close", "print", "println":
		t.note(c.Pos(), "%s has no decided form with the same meaning", id.Name)
	}
}

func (t *translator) wireBody(b *oldsyntax.WireBody) {
	for _, e := range b.Entries {
		if !e.Assign.IsValid() {
			continue
		}
		if t.src[e.Assign-1] == ' ' {
			t.replace(e.Assign-1, e.Assign+1, ":")
		} else {
			t.replace(e.Assign, e.Assign+1, ":")
		}
	}
	t.separate(b.Lbrace, b.Rbrace, nodesOf(b.Entries))
}

// tryOperands moves a try in operand position to the start of the binary
// expression: old try covered the rest of the binary expression to its
// right, decided try covers the whole expression (sixth round).
func (t *translator) tryOperands(b oldsyntax.Expr) {
	switch t.parent[b].(type) {
	case *oldsyntax.BinaryExpr, *oldsyntax.UnaryExpr:
		return
	}
	var found []*oldsyntax.TryExpr
	var walk func(x oldsyntax.Expr)
	walk = func(x oldsyntax.Expr) {
		switch x := x.(type) {
		case *oldsyntax.BinaryExpr:
			walk(x.X)
			walk(x.Y)
		case *oldsyntax.TryExpr:
			found = append(found, x)
		case *oldsyntax.UnaryExpr:
			walk(x.X)
		}
	}
	walk(b)
	if len(found) == 0 {
		return
	}
	if _, already := t.parent[b].(*oldsyntax.TryExpr); !already {
		t.insert(b.Pos(), "try ")
	}
	for _, x := range found {
		t.replace(x.Try, x.X.Pos(), "")
	}
}

// catch drops the catch binding: a single-type arm that reads it binds the
// narrowed value with `as`.
func (t *translator) catch(c *oldsyntax.CatchExpr) {
	if c.Bind == nil {
		return
	}
	t.replace(c.Bind.Pos()-1, c.Bind.End(), "")
	for _, a := range c.Arms {
		if !t.reads(a.Body, c.Bind.Name) {
			continue
		}
		if len(a.Types) != 1 {
			t.note(a.Case, "a catch arm with several types reads %s", c.Bind.Name)
			continue
		}
		t.insert(a.Types[0].End(), " as "+c.Bind.Name)
	}
}

// reads reports whether name occurs in the statements.
func (t *translator) reads(list []oldsyntax.Stmt, name string) bool {
	found := false
	for _, s := range list {
		oldsyntax.Inspect(s, func(n oldsyntax.Node) bool {
			if id, ok := n.(*oldsyntax.Ident); ok && id.Name == name {
				if sel, ok := t.parent[id].(*oldsyntax.SelectorExpr); !ok || sel.X == oldsyntax.Expr(id) {
					found = true
				}
			}
			return !found
		})
	}
	return found
}

func (t *translator) branch(b *oldsyntax.BranchStmt) {
	if b.Label != nil {
		t.note(b.Pos(), "a labelled %s: Ergo has no labels", b.Tok)
		return
	}
	if b.Tok != oldsyntax.BREAK {
		return
	}
	for p := t.parent[b]; p != nil; p = t.parent[p] {
		switch p.(type) {
		case *oldsyntax.ForStmt, *oldsyntax.RangeStmt, *oldsyntax.FuncLit, *oldsyntax.FuncDecl:
			return
		case *oldsyntax.SwitchStmt, *oldsyntax.TypeSwitchStmt, *oldsyntax.SelectStmt:
			t.note(b.Pos(), "break inside a switch arm leaves the switch in Go and the loop in Ergo")
			return
		}
	}
}

// assign rewrites `x := e` to `let [mut] x = e`, and notes the forms with
// no decided spelling: redeclaration, comma-ok, several assignments.
func (t *translator) assign(a *oldsyntax.AssignStmt) {
	switch p := t.parent[a].(type) {
	case *oldsyntax.ForStmt:
		if p.Init == oldsyntax.Stmt(a) || p.Post == oldsyntax.Stmt(a) {
			return
		}
	case *oldsyntax.IfStmt, *oldsyntax.SwitchStmt, *oldsyntax.TypeSwitchStmt:
		return // hoisted by the statement
	}
	if a.Tok != oldsyntax.DEFINE {
		if len(a.Lhs) > 1 {
			t.note(a.Pos(), "an assignment of several values: assign one name per statement")
		}
		return
	}
	t.define(a)
}

func (t *translator) define(a *oldsyntax.AssignStmt) {
	var names []*oldsyntax.Ident
	for _, l := range a.Lhs {
		id, ok := l.(*oldsyntax.Ident)
		if !ok {
			t.note(a.Pos(), "a := with a non-name on the left")
			return
		}
		names = append(names, id)
	}
	for _, id := range names {
		if id.Name != "_" && t.declaredBefore(a, id.Name) && !t.renameShadow(a, id) {
			t.note(id.Pos(), ":= redeclares %s: Ergo has no shadowing; pick a new name", id.Name)
			return
		}
	}
	if len(names) == 2 && len(a.Rhs) == 1 {
		switch r := a.Rhs[0].(type) {
		case *oldsyntax.IndexExpr, *oldsyntax.TypeGuard:
			t.note(a.Pos(), "a comma-ok form: match on the value")
			return
		case *oldsyntax.UnaryExpr:
			if r.Op == oldsyntax.ARROW {
				return
			}
		}
	}
	t.insert(a.Lhs[0].Pos(), t.letWord(names, false, a))
	t.replace(a.TokPos, a.TokPos+2, "=")
	if len(a.Rhs) > 1 {
		t.insert(a.Rhs[0].Pos(), "(")
		t.insert(a.Rhs[len(a.Rhs)-1].End(), ")")
	}
}

// renameShadow renames a := that shadows a name of an enclosing block: the
// new name replaces the declared one in the rest of its block, which is
// exactly its scope. It refuses (false) a redeclaration in the same block,
// which Go reads as an assignment, and a block that declares the name again.
func (t *translator) renameShadow(a *oldsyntax.AssignStmt, id *oldsyntax.Ident) bool {
	var list []oldsyntax.Stmt
	switch p := t.parent[a].(type) {
	case *oldsyntax.BlockStmt:
		list = p.List
	case *oldsyntax.CaseClause:
		list = p.Body
	case *oldsyntax.CatchArm:
		list = p.Body
	default:
		return false
	}
	k := -1
	for i, s := range list {
		if s == oldsyntax.Stmt(a) {
			k = i
		}
	}
	if k < 0 {
		return false
	}
	declares := func(s oldsyntax.Node) bool {
		found := false
		oldsyntax.Inspect(s, func(n oldsyntax.Node) bool {
			switch n := n.(type) {
			case *oldsyntax.AssignStmt:
				if n.Tok == oldsyntax.DEFINE {
					for _, l := range n.Lhs {
						if x, ok := l.(*oldsyntax.Ident); ok && x.Name == id.Name && x != id {
							found = true
						}
					}
				}
			case *oldsyntax.VarSpec:
				for _, x := range n.Names {
					found = found || x.Name == id.Name
				}
			case *oldsyntax.Field:
				for _, x := range n.Names {
					found = found || x.Name == id.Name
				}
			}
			return !found
		})
		return found
	}
	for _, s := range list[:k] {
		if declares(s) {
			return false
		}
	}
	for _, s := range list[k+1:] {
		if declares(s) {
			return false
		}
	}
	used := map[string]bool{}
	oldsyntax.Inspect(t.file, func(n oldsyntax.Node) bool {
		if x, ok := n.(*oldsyntax.Ident); ok {
			used[x.Name] = true
		}
		return true
	})
	name := ""
	for i := 2; ; i++ {
		name = fmt.Sprintf("%s%d", id.Name, i)
		if !used[name] {
			break
		}
	}
	t.replace(id.Pos(), id.End(), name)
	for _, s := range list[k+1:] {
		oldsyntax.Inspect(s, func(n oldsyntax.Node) bool {
			x, ok := n.(*oldsyntax.Ident)
			if !ok || x.Name != id.Name {
				return true
			}
			switch p := t.parent[x].(type) {
			case *oldsyntax.SelectorExpr:
				if p.Sel == x {
					return true
				}
			case *oldsyntax.KeyValueExpr:
				if _, lit := t.parent[p].(*oldsyntax.CompositeLit); lit && p.Key == oldsyntax.Expr(x) {
					return true
				}
			}
			t.replace(x.Pos(), x.End(), name)
			return true
		})
	}
	return true
}

// declaredBefore reports whether name is declared in the function before
// at: a parameter, receiver, earlier := or var, or a range variable.
func (t *translator) declaredBefore(at oldsyntax.Node, name string) bool {
	fn := t.enclosingFunc(at)
	if fn == nil {
		return false
	}
	found := false
	for f := fn; f != nil && !found; f = t.enclosingFunc(f) {
		oldsyntax.Inspect(f, func(n oldsyntax.Node) bool {
			if n.Pos() >= at.Pos() {
				return false
			}
			switch n := n.(type) {
			case *oldsyntax.Field:
				for _, id := range n.Names {
					found = found || id.Name == name
				}
			case *oldsyntax.AssignStmt:
				if n.Tok == oldsyntax.DEFINE {
					for _, l := range n.Lhs {
						if id, ok := l.(*oldsyntax.Ident); ok && id.Name == name {
							found = true
						}
					}
				}
			case *oldsyntax.VarSpec:
				for _, id := range n.Names {
					found = found || id.Name == name
				}
			case *oldsyntax.RangeStmt:
				for _, x := range []oldsyntax.Expr{n.Key, n.Value} {
					if id, ok := x.(*oldsyntax.Ident); ok && id.Name == name && n.Tok == oldsyntax.DEFINE {
						found = true
					}
				}
			}
			return !found
		})
	}
	return found
}

// hoistInit writes an if or switch init statement as a let on the line
// before the statement.
func (t *translator) hoistInit(stmtPos oldsyntax.Pos, init oldsyntax.Stmt, next oldsyntax.Pos) {
	a, ok := init.(*oldsyntax.AssignStmt)
	if !ok || a.Tok != oldsyntax.DEFINE || len(a.Rhs) != 1 {
		t.note(init.Pos(), "an init statement: write it on the line before")
		return
	}
	for _, l := range a.Lhs {
		if id, ok := l.(*oldsyntax.Ident); !ok || t.declaredBefore(a, id.Name) || t.usedAfter(t.parent[a], id.Name) {
			t.note(init.Pos(), "an init statement whose name clashes: write the let by hand")
			return
		}
	}
	if _, isIndex := a.Rhs[0].(*oldsyntax.IndexExpr); isIndex && len(a.Lhs) == 2 {
		t.note(init.Pos(), "a comma-ok form: match on the value")
		return
	}
	names := make([]*oldsyntax.Ident, len(a.Lhs))
	for i, l := range a.Lhs {
		names[i] = l.(*oldsyntax.Ident)
	}
	t.skip(init)
	indent := t.indentOf(stmtPos)
	t.insert(stmtPos, t.letWord(names, false, a)+t.src[a.Lhs[0].Pos():a.TokPos]+"= "+t.text(a.Rhs[0])+"\n"+indent)
	t.replace(init.Pos(), next, "")
}

// usedAfter reports whether name is used after the statement s in its
// function: a hoisted let would then be in scope where the init's was not.
func (t *translator) usedAfter(s oldsyntax.Node, name string) bool {
	fn := t.enclosingFunc(s)
	if fn == nil {
		return false
	}
	found := false
	oldsyntax.Inspect(fn, func(n oldsyntax.Node) bool {
		if id, ok := n.(*oldsyntax.Ident); ok && id.Name == name && id.Pos() >= s.End() {
			found = true
		}
		return !found
	})
	return found
}

func (t *translator) indentOf(pos oldsyntax.Pos) string {
	start := strings.LastIndexByte(t.src[:pos], '\n') + 1
	line := t.src[start:pos]
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// forStmt rewrites `for i := a; i < b; i++` to `for i in a..b` when i has
// no other write; any other three-clause for is a note.
func (t *translator) forStmt(f *oldsyntax.ForStmt) {
	if f.Init == nil && f.Post == nil {
		return
	}
	init, ok1 := f.Init.(*oldsyntax.AssignStmt)
	cond, ok2 := f.Cond.(*oldsyntax.BinaryExpr)
	post, ok3 := f.Post.(*oldsyntax.IncDecStmt)
	if ok1 && ok2 && ok3 && init.Tok == oldsyntax.DEFINE && len(init.Lhs) == 1 && len(init.Rhs) == 1 && cond.Op == oldsyntax.LSS && post.Tok == oldsyntax.INC {
		id, _ := init.Lhs[0].(*oldsyntax.Ident)
		cx, _ := cond.X.(*oldsyntax.Ident)
		px, _ := post.X.(*oldsyntax.Ident)
		if id != nil && cx != nil && px != nil && cx.Name == id.Name && px.Name == id.Name && !t.written(f.Body, id.Name) {
			t.replace(f.Init.Pos(), f.Body.Pos(), id.Name+" in "+t.text(init.Rhs[0])+".."+t.text(cond.Y)+" ")
			t.skip(f.Init)
			t.skip(f.Post)
			return
		}
	}
	t.note(f.Pos(), "a three-clause for: write for i in a..b or a for cond loop")
}

func (t *translator) rangeStmt(r *oldsyntax.RangeStmt) {
	if r.Tok == oldsyntax.ASSIGN {
		t.note(r.Pos(), "a range assigning existing names: bind new names with for x in xs")
		return
	}
	key, _ := r.Key.(*oldsyntax.Ident)
	val, _ := r.Value.(*oldsyntax.Ident)
	for _, id := range []*oldsyntax.Ident{key, val} {
		if id != nil {
			t.keywordName(id)
		}
	}
	var head string
	switch {
	case r.Key == nil:
		head = "_ in "
	case r.Value != nil && key != nil && key.Name == "_":
		head = t.text(r.Value) + " in "
	case r.Value != nil:
		head = t.text(r.Key) + ", " + t.text(r.Value) + " in "
	case t.isCount(r.X):
		head = t.text(r.Key) + " in 0.."
	case t.isSequence(r.X):
		head = t.text(r.Key) + " in "
	default:
		head = t.text(r.Key) + ", _ in "
	}
	t.replace(r.For+3, r.X.Pos(), " "+head)
}

// isCount reports whether a range expression is an integer: a literal, a
// len call, or a parameter of an integer type.
func (t *translator) isCount(x oldsyntax.Expr) bool {
	switch x := x.(type) {
	case *oldsyntax.BasicLit:
		return x.Kind == oldsyntax.INT
	case *oldsyntax.CallExpr:
		id, ok := x.Fun.(*oldsyntax.Ident)
		return ok && id.Name == "len"
	case *oldsyntax.Ident:
		return t.paramType(x, func(ty oldsyntax.Expr) bool {
			id, ok := ty.(*oldsyntax.Ident)
			return ok && strings.HasPrefix(id.Name, "int")
		})
	}
	return false
}

// isSequence reports whether a range expression yields elements with one
// variable: a call (a stream or sequence) or a channel parameter.
func (t *translator) isSequence(x oldsyntax.Expr) bool {
	switch x := x.(type) {
	case *oldsyntax.CallExpr, *oldsyntax.TryExpr:
		return true
	case *oldsyntax.Ident:
		return t.paramType(x, func(ty oldsyntax.Expr) bool {
			_, ok := ty.(*oldsyntax.ChanType)
			return ok
		})
	}
	return false
}

func (t *translator) paramType(x *oldsyntax.Ident, test func(oldsyntax.Expr) bool) bool {
	fn, ok := t.enclosingFunc(x).(*oldsyntax.FuncDecl)
	if !ok {
		return false
	}
	for _, f := range fn.Type.Params.List {
		for _, n := range f.Names {
			if n.Name == x.Name {
				return test(f.Type)
			}
		}
	}
	return false
}

// switchStmt rewrites a value switch: all-literal cases become a match,
// anything else an if chain.
func (t *translator) switchStmt(s *oldsyntax.SwitchStmt) {
	if s.Init != nil {
		next := s.Lbrace
		if s.Tag != nil {
			next = s.Tag.Pos()
		}
		t.hoistInit(s.Switch, s.Init, next)
	}
	literal := s.Tag != nil
	for _, c := range s.Clauses {
		for _, x := range c.List {
			if u, ok := x.(*oldsyntax.UnaryExpr); ok && u.Op == oldsyntax.SUB {
				x = u.X
			}
			if _, ok := x.(*oldsyntax.BasicLit); !ok {
				literal = false
			}
		}
	}
	if literal {
		t.replace(s.Switch, s.Switch+6, "match")
		for _, c := range s.Clauses {
			if c.List == nil {
				t.replace(c.Case, c.Case+7, "case _")
			}
		}
		return
	}
	tag := ""
	if s.Tag != nil {
		if _, isName := s.Tag.(*oldsyntax.Ident); !isName {
			t.note(s.Pos(), "a switch on an expression with non-literal cases: bind the subject with let, then write an if chain")
			return
		}
		tag = t.text(s.Tag)
	}
	for i, c := range s.Clauses {
		if c.List == nil && i != len(s.Clauses)-1 {
			t.note(c.Case, "default before the last case: move it last")
			return
		}
	}
	if len(s.Clauses) == 0 {
		t.note(s.Pos(), "an empty switch")
		return
	}
	start := s.Switch
	if s.Init != nil {
		t.replace(s.Switch, s.Init.Pos(), "")
		start = s.Lbrace
		if s.Tag != nil {
			start = s.Tag.Pos()
		}
	}
	t.replace(start, s.Lbrace+1, "")
	for i, c := range s.Clauses {
		var head string
		switch {
		case c.List == nil:
			head = "} else {"
		default:
			conds := make([]string, len(c.List))
			for j, x := range c.List {
				conds[j] = t.text(x)
				if tag != "" {
					conds[j] = tag + " == " + conds[j]
				} else if len(c.List) > 1 {
					conds[j] = "(" + conds[j] + ")"
				}
			}
			head = "if " + strings.Join(conds, " || ") + " {"
			if i > 0 {
				head = "} else " + head
			}
		}
		t.replace(c.Case, c.Colon+1, head)
		t.skipList(c.List)
	}
}

func (t *translator) skipList(list []oldsyntax.Expr) {
	for _, x := range list {
		t.skip(x)
	}
}

// typeSwitch rewrites `switch p := x.(type)` to `match x`, binding p with
// `as` in the single-type arms that read it.
func (t *translator) typeSwitch(s *oldsyntax.TypeSwitchStmt) {
	from := s.Subjects[0].Pos()
	if len(s.Binds) > 0 {
		from = s.Binds[0].Pos()
	}
	if s.Init != nil {
		t.hoistInit(s.Switch, s.Init, from)
	}
	subjects := make([]string, len(s.Subjects))
	for i, g := range s.Subjects {
		subjects[i] = t.text(g.X)
	}
	head := "match " + subjects[0] + " "
	if len(subjects) > 1 {
		head = "match (" + strings.Join(subjects, ", ") + ") "
	}
	if s.Init == nil {
		t.replace(s.Switch, from, "")
	} else {
		t.replace(s.Switch, s.Init.Pos(), "")
	}
	t.replace(from, s.Lbrace, head)
	for _, g := range s.Subjects {
		t.skip(g)
	}
	for _, c := range s.Clauses {
		if c.List == nil {
			t.replace(c.Case, c.Case+7, "case _")
		}
		for i, b := range s.Binds {
			if !t.reads(c.Body, b.Name) {
				continue
			}
			if len(c.List) == 1 && len(s.Binds) == 1 {
				t.insert(c.List[0].End(), " as "+b.Name)
				continue
			}
			subject, isName := s.Subjects[i].X.(*oldsyntax.Ident)
			if !isName {
				t.note(c.Case, "an arm with several types reads %s: bind the subject with let first", b.Name)
				continue
			}
			t.renameReads(c.Body, b.Name, subject.Name)
		}
	}
}

func (t *translator) renameReads(list []oldsyntax.Stmt, from, to string) {
	for _, s := range list {
		oldsyntax.Inspect(s, func(n oldsyntax.Node) bool {
			if id, ok := n.(*oldsyntax.Ident); ok && id.Name == from {
				if sel, ok := t.parent[id].(*oldsyntax.SelectorExpr); !ok || sel.X == oldsyntax.Expr(id) {
					t.replace(id.Pos(), id.End(), to)
				}
			}
			return true
		})
	}
}
