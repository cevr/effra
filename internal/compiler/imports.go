package compiler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/importer"
	gotoken "go/token"
	"go/types"
	"hash"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type GoImport struct {
	Alias string `json:"alias"`
	Path  string `json:"path"`
	Span  Span   `json:"span"`
}
type Binding struct {
	// Symbol is the source display, such as host.Find or (*host.Counter).Add.
	// Identity is the canonical go/types identity that keys bindings and
	// checked calls: go:<full name> for a function, and for a method its
	// actual receiver and name, go:(*example.com/sdk.Client).Add.
	Symbol       string   `json:"symbol"`
	Identity     string   `json:"identity"`
	Package      string   `json:"package"`
	Signature    string   `json:"hostSignature"`
	Params       []string `json:"parameters"`
	Return       string   `json:"return"`
	HasError     bool     `json:"hasError"`
	Context      bool     `json:"forwardContext"`
	Cancellation string   `json:"cancellation"`
	Provenance   string   `json:"provenance"`
	// Protocol names the standard I/O method contract a method call follows
	// (io.Reader, io.ReaderAt or io.Writer); its buffer and count adapt.
	Protocol string `json:"protocol,omitempty"`
	// HostParameters and HostResults are the complete native signature with
	// each component's explicit adaptation, including nullability.
	HostParameters []HostComponent `json:"hostParameters"`
	HostResults    []HostComponent `json:"hostResults"`
	// member is the exported Go name; a call site supplies its own import alias.
	member string
	// receiver is the admitted receiver type of a method binding.
	receiver *hostType
	// params and results are the admitted native types, excluding a forwarded
	// context and the trailing error.
	params  []hostType
	results []hostType
}
type behavior struct {
	Context      string `json:"context"`
	Cancellation string `json:"cancellation"`
}
type goModule struct {
	Path, Version, Dir, GoMod string
	Main                      bool
	Replace                   *goModule
}
type listedPackage struct {
	ImportPath, Export, Name string
	Standard                 bool
	Module                   *goModule
	Error                    *struct{ Err string }
}

// generatedModulePath is the module of every generated Go program. Its main
// package lives at the module root, so it is also the import path of the
// package that imports declared foreign packages.
const generatedModulePath = "effra.generated"

// goImportRefusal applies the Go command's import restrictions
// (cmd/go/internal/load: disallowInternal, disallowVendor and the program
// check) to pkg as imported by the generated main package. go list exempts
// packages named on its command line from these rules, so loading export
// data does not establish that the generated program may import them.
func goImportRefusal(pkg listedPackage) string {
	if index, ok := goInternalElement(pkg.ImportPath); ok {
		// A module package is visible to importers below the parent of its
		// final internal element. A standard package is visible only inside
		// GOROOT, which never contains the generated module.
		parent := strings.TrimSuffix(pkg.ImportPath[:index], "/")
		if pkg.Standard || !hasGoPathPrefix(generatedModulePath, parent) {
			return "use of internal package " + pkg.ImportPath + " not allowed from the generated module " + generatedModulePath
		}
	}
	if strings.HasPrefix(pkg.ImportPath, "vendor/") || strings.Contains(pkg.ImportPath, "/vendor/") {
		return "use of vendored package " + pkg.ImportPath + " not allowed"
	}
	if pkg.Name == "main" {
		return "import " + strconv.Quote(pkg.ImportPath) + " is a program, not an importable package"
	}
	return ""
}

// goInternalElement returns the index of the final "internal" element of an
// import path, matching cmd/go's findInternal.
func goInternalElement(path string) (int, bool) {
	switch {
	case strings.HasSuffix(path, "/internal"):
		return len(path) - len("internal"), true
	case strings.Contains(path, "/internal/"):
		return strings.LastIndex(path, "/internal/") + 1, true
	case path == "internal", strings.HasPrefix(path, "internal/"):
		return 0, true
	}
	return 0, false
}

// hasGoPathPrefix reports whether path is prefix or lies below it, matching
// cmd/go's str.HasPathPrefix.
func hasGoPathPrefix(path, prefix string) bool {
	return prefix == "" || path == prefix || strings.HasPrefix(path, prefix+"/")
}

func goCommand(dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go %s: %v: %s", args[0], err, stderr.String())
	}
	return out, nil
}

// Import loading uses go list's module resolution and compiled export data.
// There is no guessed handwritten foreign signature and no source dependency walk.
func (r *Result) loadImports(dir string) {
	if len(r.Program.Imports) == 0 {
		return
	}
	r.Program.GoOnly = true
	if r.Target != "go" {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF110", Message: "Go imports require the Go target"})
		return
	}
	start := time.Now()
	defer func() { r.Timings.ImportMicros = time.Since(start).Microseconds() }()
	args := []string{"list", "-deps", "-export", "-json", "--"}
	for _, imp := range r.Program.Imports {
		args = append(args, imp.Path)
	}
	data, err := goCommand(dir, args...)
	if err != nil {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF111", Message: err.Error()})
		return
	}
	listed := map[string]listedPackage{}
	modules := map[string]*goModule{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var pkg listedPackage
		err = decoder.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF111", Message: err.Error()})
			return
		}
		listed[pkg.ImportPath] = pkg
		if pkg.Module != nil {
			modules[pkg.Module.Path] = pkg.Module
		}
	}
	// exports maps a package to its export data. Contract-key checking adds
	// the packages it lists beyond the import closure, so the loader reads it.
	exports := make(map[string]string, len(listed))
	for path, pkg := range listed {
		exports[path] = pkg.Export
	}
	loader := importer.ForCompiler(gotoken.NewFileSet(), "gc", func(path string) (io.ReadCloser, error) {
		if exports[path] == "" {
			return nil, fmt.Errorf("missing export data for %s", path)
		}
		return os.Open(exports[path])
	})
	contracts, main, err := loadContracts(dir)
	if err != nil {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF111", Message: err.Error()})
		return
	}
	if main.Dir != "" {
		modules[main.Path] = main
		r.ModuleSum, err = os.ReadFile(filepath.Join(main.Dir, "go.sum"))
		if err != nil && !os.IsNotExist(err) {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF111", Message: err.Error()})
			return
		}
	}
	// Export bytes participate in the semantic revision, including dependency changes.
	hash := sha256.New()
	hash.Write([]byte(r.Revision))
	contractData, _ := json.Marshal(contracts)
	hash.Write(contractData)
	// Refused keys are removed before any binding is admitted, so no binding
	// or inspection carries a contract its metadata failed to name.
	r.Diagnostics = append(r.Diagnostics, checkContractKeys(dir, contracts, exports, loader, hash)...)
	host := &hostImports{aliases: map[string]string{}, types: map[string]*types.TypeName{}, unsupported: map[string]string{}, contracts: contracts}
	r.Program.host = host
	for _, imp := range r.Program.Imports {
		if _, exists := host.aliases[imp.Path]; !exists {
			host.aliases[imp.Path] = imp.Alias
		}
	}
	for _, imp := range r.Program.Imports {
		// Every declared import is emitted, named or blank, so a package the
		// generated program cannot import is refused even when uncalled. Its
		// declarations still load, so calls through it report no second error.
		if refusal := goImportRefusal(listed[imp.Path]); refusal != "" {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF111", Message: refusal, Span: imp.Span})
		}
		pkg, err := loader.Import(imp.Path)
		if err != nil {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF111", Message: err.Error(), Span: imp.Span})
			continue
		}
		archive, err := os.ReadFile(listed[imp.Path].Export)
		if err != nil {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF111", Message: err.Error(), Span: imp.Span})
			continue
		}
		hash.Write([]byte(imp.Path))
		hash.Write(archive)
		for _, name := range pkg.Scope().Names() {
			switch member := pkg.Scope().Lookup(name).(type) {
			case *types.TypeName:
				if member.Exported() {
					host.types[imp.Alias+"."+name] = member
				}
			case *types.Func:
				if !member.Exported() {
					continue
				}
				if b, unsupported := normalizeBinding(imp, member, host); unsupported != "" {
					host.unsupported[b.Symbol] = unsupported
				} else {
					r.Program.Bindings[b.Identity] = b
				}
			}
		}
	}
	r.Revision = hex.EncodeToString(hash.Sum(nil))
	for _, path := range slices.Sorted(maps.Keys(modules)) {
		r.Program.Modules = append(r.Program.Modules, modules[path])
	}
}
func loadContracts(dir string) (map[string]behavior, *goModule, error) {
	data, err := goCommand(dir, "list", "-m", "-json")
	if err != nil {
		return nil, nil, err
	}
	var main goModule
	if err = json.Unmarshal(data, &main); err != nil {
		return nil, nil, err
	}
	contracts := map[string]behavior{}
	if main.Dir == "" {
		return contracts, &main, nil
	}
	content, err := os.ReadFile(filepath.Join(main.Dir, "effra.bindings.json"))
	if os.IsNotExist(err) {
		return contracts, &main, nil
	}
	if err != nil {
		return nil, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&contracts); err != nil {
		return nil, nil, fmt.Errorf("invalid effra.bindings.json: %w", err)
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return nil, nil, fmt.Errorf("effra.bindings.json must contain one JSON object")
	}
	// Keys are validated in sorted order so a load reports the same first
	// error every time.
	for _, symbol := range slices.Sorted(maps.Keys(contracts)) {
		meta := contracts[symbol]
		if meta.Context != "" && meta.Context != "fiber" {
			return nil, nil, fmt.Errorf("unsupported context contract for %s", symbol)
		}
		if meta.Cancellation != "" && meta.Cancellation != "cooperative" && meta.Cancellation != "unknown" {
			return nil, nil, fmt.Errorf("unsupported cancellation contract for %s", symbol)
		}
	}
	return contracts, &main, nil
}

// checkContractKeys refuses every effra.bindings.json key that is not the
// go/types full name of an exported function or method, as metadata loading
// refuses an unknown field. A key resolves in the package it names, which
// source need not import, so packages outside the import closure are listed
// separately; their export data joins the semantic revision. A method of an
// unnamed interface is spelled (interface).M whatever its package or
// signature, so such a key is refused: it names no single declaration.
// Diagnostics are sorted by key and name near misses from the same package;
// every refused key is removed from contracts.
func checkContractKeys(dir string, contracts map[string]behavior, exports map[string]string, loader types.Importer, revision hash.Hash) []Diagnostic {
	keys := slices.Sorted(maps.Keys(contracts))
	packages := map[string]string{}
	missing := map[string]bool{}
	for _, key := range keys {
		if path, ok := contractKeyPackage(key); ok {
			packages[key] = path
			if path != "" && exports[path] == "" {
				missing[path] = true
			}
		}
	}
	failed := map[string]string{}
	if len(missing) > 0 {
		data, err := goCommand(dir, append([]string{"list", "-e", "-deps", "-export", "-json", "--"}, slices.Sorted(maps.Keys(missing))...)...)
		if err != nil {
			for key, path := range packages {
				if missing[path] {
					delete(contracts, key)
				}
			}
			return []Diagnostic{{Code: "EF111", Message: err.Error()}}
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		for {
			var pkg listedPackage
			if err = decoder.Decode(&pkg); err == io.EOF {
				break
			} else if err != nil {
				return []Diagnostic{{Code: "EF111", Message: err.Error()}}
			}
			if pkg.Error != nil {
				failed[pkg.ImportPath] = strings.TrimSpace(pkg.Error.Err)
			} else if exports[pkg.ImportPath] == "" {
				exports[pkg.ImportPath] = pkg.Export
			}
		}
	}
	scopes := map[string]*types.Scope{"": types.Universe}
	for _, path := range slices.Sorted(maps.Values(packages)) {
		if _, loaded := scopes[path]; loaded || failed[path] != "" {
			continue
		}
		pkg, err := loader.Import(path)
		if err != nil {
			failed[path] = err.Error()
			continue
		}
		scopes[path] = pkg.Scope()
		if archive, err := os.ReadFile(exports[path]); err == nil {
			revision.Write([]byte(path))
			revision.Write(archive)
		}
	}
	var diagnostics []Diagnostic
	refuse := func(key, message string) {
		delete(contracts, key)
		diagnostics = append(diagnostics, Diagnostic{Code: "EF111", Message: message})
	}
	for _, key := range keys {
		if strings.HasPrefix(key, "(interface).") {
			refuse(key, fmt.Sprintf("effra.bindings.json key %q names a method of an unnamed interface, which Go spells this way whatever its package or signature, so it names no single declaration and carries no contract; pass an explicit context.Context argument, or declare a named Go interface with the method in the module and key that method", key))
			continue
		}
		path, ok := packages[key]
		if !ok {
			refuse(key, fmt.Sprintf("effra.bindings.json key %q is not a go/types full name such as %q or %q", key, "path.Func", "(*path.Type).Method"))
			continue
		}
		scope := scopes[path]
		if scope == nil {
			refuse(key, fmt.Sprintf("effra.bindings.json key %q names package %s, which does not load: %s", key, path, failed[path]))
			continue
		}
		declared := contractDeclarations(scope)
		if declared[key] {
			continue
		}
		message := fmt.Sprintf("effra.bindings.json key %q matches no Go function or method", key)
		if near := contractNearMisses(key, scope, declared); len(near) > 0 {
			quoted := make([]string, len(near))
			for i, name := range near {
				quoted[i] = strconv.Quote(name)
			}
			message += "; near: " + strings.Join(quoted, ", ")
		}
		refuse(key, message)
	}
	return diagnostics
}

// contractKeyPackage returns the package a full name belongs to: the text
// before a function's last dot, or before a method receiver type's last dot
// once its pointer and type parameters are removed. A receiver without a
// package names the universe, as in (error).Error.
func contractKeyPackage(key string) (string, bool) {
	target := key
	if strings.HasPrefix(key, "(") {
		end := strings.LastIndex(key, ").")
		if end < 0 || end+2 == len(key) {
			return "", false
		}
		target = strings.TrimPrefix(key[1:end], "*")
		if open := strings.IndexByte(target, '['); open >= 0 {
			target = target[:open]
		}
		if target != "" && !strings.Contains(target, ".") {
			return "", true
		}
	}
	dot := strings.LastIndex(target, ".")
	if dot <= 0 || dot == len(target)-1 {
		return "", false
	}
	return target[:dot], true
}

// contractDeclarations is the set of full names a contract can key in one
// scope: exported functions and exported methods, including methods declared
// on unexported types that exported types promote.
func contractDeclarations(scope *types.Scope) map[string]bool {
	declared := map[string]bool{}
	for _, name := range scope.Names() {
		switch object := scope.Lookup(name).(type) {
		case *types.Func:
			if object.Exported() {
				declared[object.FullName()] = true
			}
		case *types.TypeName:
			named, _ := object.Type().(*types.Named)
			if named == nil {
				continue
			}
			for i := range named.NumMethods() {
				if method := named.Method(i); method.Exported() {
					declared[method.FullName()] = true
				}
			}
			if iface, ok := named.Underlying().(*types.Interface); ok {
				for i := range iface.NumExplicitMethods() {
					if method := iface.ExplicitMethod(i); method.Exported() {
						declared[method.FullName()] = true
					}
				}
			}
		}
	}
	return declared
}

// contractNearMisses lists at most three declarations a mistyped key likely
// meant: the declaration of the method Go selects for the key's receiver and
// method (a promoted method, an instance of a generic declaration, or the
// other receiver form), then full names within
// edit distance two.
func contractNearMisses(key string, scope *types.Scope, declared map[string]bool) []string {
	near := map[string]bool{}
	if end := strings.LastIndex(key, ")."); strings.HasPrefix(key, "(") && end > 0 {
		receiver := strings.TrimPrefix(key[1:end], "*")
		if open := strings.IndexByte(receiver, '['); open >= 0 {
			receiver = receiver[:open]
		}
		if object, ok := scope.Lookup(receiver[strings.LastIndex(receiver, ".")+1:]).(*types.TypeName); ok {
			for _, t := range []types.Type{object.Type(), types.NewPointer(object.Type())} {
				if selection := types.NewMethodSet(t).Lookup(nil, key[end+2:]); selection != nil {
					if name := selection.Obj().(*types.Func).Origin().FullName(); declared[name] && name != key {
						near[name] = true
					}
				}
			}
		}
	}
	for name := range declared {
		if editDistance(key, name) <= 2 {
			near[name] = true
		}
	}
	names := slices.Sorted(maps.Keys(near))
	return names[:min(len(names), 3)]
}

// editDistance is the Levenshtein distance between two strings, in bytes.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := range len(a) {
		current := make([]int, len(b)+1)
		current[0] = i + 1
		for j := range len(b) {
			cost := 1
			if a[i] == b[j] {
				cost = 0
			}
			current[j+1] = min(previous[j+1]+1, current[j]+1, previous[j]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

// normalizeBinding admits one exported function from its native signature. A
// refused function returns the reason, reported when source calls it.
func normalizeBinding(imp GoImport, fn *types.Func, host *hostImports) (Binding, string) {
	b := Binding{Symbol: imp.Alias + "." + fn.Name(), Identity: "go:" + imp.Path + "." + fn.Name(), Package: imp.Path}
	return b, admitCallable(&b, fn, host)
}

// admitCallable admits the native signature of an exported function or
// method into b: its parameters, complete results and behavior contract. A
// contract is keyed by the go/types full name of the declaration, so a
// promoted method carries the contract of the method it promotes.
func admitCallable(b *Binding, fn *types.Func, host *hostImports) string {
	// Behavior belongs to the declaration: a method promoted from an embedded
	// instance of a generic type is a synthetic Func whose full name spells
	// the instance, so its contract is the origin's, while the signature
	// stays the instantiated one.
	sig := fn.Type().(*types.Signature)
	meta := host.contracts[fn.Origin().FullName()]
	b.Signature, b.Cancellation, b.Provenance, b.member = sig.String(), "unknown", "Go export data; behavior unclassified", fn.Name()
	if meta.Cancellation != "" {
		b.Cancellation = meta.Cancellation
		b.Provenance = "Go export data; reviewed effra.bindings.json assertion"
	}
	if sig.TypeParams().Len() > 0 {
		return "generic Go functions are unsupported"
	}
	if sig.Variadic() {
		return "variadic Go functions are unsupported"
	}
	b.Params, b.HostParameters, b.HostResults = []string{}, []HostComponent{}, []HostComponent{}
	for i := 0; i < sig.Params().Len(); i++ {
		typ := sig.Params().At(i).Type()
		if i == 0 && isContext(typ) && meta.Context == "fiber" {
			b.Context = true
			b.Provenance = "Go export data; reviewed effra.bindings.json assertion"
			b.HostParameters = append(b.HostParameters, HostComponent{Native: "context.Context", Type: "Fiber context", Adaptation: hostAdaptContext})
			continue
		}
		admitted, err := admitHostType(typ)
		if err != nil {
			return fmt.Sprintf("parameter %d: %v", i+1, err)
		}
		b.params = append(b.params, admitted)
		b.Params = append(b.Params, host.adaptedDisplay(admitted, false))
		b.HostParameters = append(b.HostParameters, host.component(admitted, false))
	}
	// Only a trailing error is an error result; every other component,
	// including a (T, bool) flag, is retained as an ordinary value.
	results := sig.Results()
	count := results.Len()
	if count > 0 && types.Identical(results.At(count-1).Type(), hostErrorType.native) {
		b.HasError = true
		count--
	}
	displays := []string{}
	for i := 0; i < count; i++ {
		admitted, err := admitHostType(results.At(i).Type())
		if err != nil {
			return fmt.Sprintf("result %d: %v", i+1, err)
		}
		b.results = append(b.results, admitted)
		component := host.component(admitted, true)
		b.HostResults = append(b.HostResults, component)
		displays = append(displays, component.Type)
	}
	if b.HasError {
		b.HostResults = append(b.HostResults, HostComponent{Native: "error", Type: "GoResult", Adaptation: hostAdaptError})
	}
	switch len(displays) {
	case 0:
		b.Return = voidTypeName
	case 1:
		b.Return = displays[0]
	default:
		b.Return = "(" + strings.Join(displays, ", ") + ")"
	}
	if meta.Context == "fiber" && !b.Context {
		return "the context contract requires a first context.Context parameter"
	}
	if meta.Cancellation == "cooperative" && !b.Context {
		return "cooperative cancellation requires context forwarding"
	}
	return ""
}

func isContext(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "context" && named.Obj().Name() == "Context"
}

func (c *checker) foreignCall(e *Expr, env localEnv, inEffect bool) bool {
	if e.Left.Kind != "member" || e.Left.Left.Kind != "name" {
		return false
	}
	alias := e.Left.Left.Name
	for _, imp := range c.program.Imports {
		if imp.Alias != alias {
			continue
		}
		if e.Left.Left.binding != nil {
			c.diagnostic("EF103", "local shadows Go import "+alias, e.Span)
			return true
		}
		key := alias + "." + e.Left.Name
		b, ok := c.program.Bindings["go:"+imp.Path+"."+e.Left.Name]
		var host hostBindingTypes
		if ok {
			host, ok = c.hostBinding(b)
			if !ok {
				c.diagnostic("EF112", "Go symbol "+key+" requires bundled "+hostOptionModule+" "+hostOptionMember+" for absence adaptation", e.Span)
			}
		} else if reason := c.program.host.unsupportedReason(key); reason != "" {
			c.diagnostic("EF112", "unsupported Go symbol "+key+": "+reason, e.Span)
		} else {
			c.diagnostic("EF112", "unknown Go symbol "+key, e.Span)
		}
		if !ok {
			e.checked = c.checkedData("invalid")
			e.Type = c.projectChecked(e.checked)
			return true
		}
		c.program.UsedImports[alias] = true
		c.checkForeignCall(e, b, host, env, inEffect)
		return true
	}
	return false
}

// checkForeignCall types one admitted native call as a lazy Foreign recipe.
// Arguments are checked against the native parameters with Go's assignment
// rule, so a concrete host value reaches a native interface directly.
func (c *checker) checkForeignCall(e *Expr, b Binding, host hostBindingTypes, env localEnv, inEffect bool) {
	if len(e.Args) != len(host.params) {
		c.diagnostic("EF106", "incorrect Go argument count for "+b.Symbol, e.Span)
	}
	for i, arg := range e.Args {
		actual := c.expr(arg, env, inEffect)
		if i >= len(host.params) || c.hostIntegerLiteral(arg, host.params[i]) {
			continue
		}
		if actual.isEffect() || !c.hostAssignable(actual.valueID(), host.params[i]) {
			c.diagnostic("EF106", "Go argument must be "+c.displayTypeID(host.params[i]), arg.Span)
		}
	}
	t := checkedExpression{value: c.values.recipe(host.result, nil, checkedEffectCallable, emptyRowID, c.internRow([]string{"Foreign"}), nil, nil)}
	e.checked = t.clone()
	e.Type = c.projectChecked(t)
	e.Text = "foreign"
	e.Name = b.Identity
	for _, existing := range c.result.Bindings {
		if existing.Identity == b.Identity {
			return
		}
	}
	c.result.Bindings = append(c.result.Bindings, b)
}

// ModuleFile preserves the source workspace's module graph for generated code.
func (r *Result) ModuleFile() []byte {
	var out strings.Builder
	out.WriteString("module " + generatedModulePath + "\n\ngo 1.27\n")
	modules := append([]*goModule{}, r.Program.Modules...)
	// Sorted output avoids rewriting the generated module on unchanged builds.
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	for _, m := range modules {
		version := m.Version
		if m.Main {
			version = "v0.0.0"
			tail := filepath.Base(m.Path)
			if strings.HasPrefix(tail, "v") {
				if major, err := strconv.Atoi(tail[1:]); err == nil && major >= 2 {
					version = tail + ".0.0"
				}
			}
		}
		if version != "" {
			out.WriteString("require " + m.Path + " " + version + "\n")
		}
		replacement := m.Replace
		if m.Main {
			replacement = &goModule{Dir: m.Dir}
		}
		if replacement != nil {
			dest := replacement.Path
			if replacement.Dir != "" && replacement.Version == "" {
				dest = fmt.Sprintf("%q", replacement.Dir)
			}
			out.WriteString("replace " + m.Path + " => " + dest)
			if replacement.Version != "" {
				out.WriteString(" " + replacement.Version)
			}
			out.WriteString("\n")
		}
	}
	return []byte(out.String())
}
