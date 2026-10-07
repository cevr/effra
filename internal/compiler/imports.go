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
	Symbol       string   `json:"symbol"`
	Package      string   `json:"package"`
	Signature    string   `json:"hostSignature"`
	Params       []string `json:"parameters"`
	Return       string   `json:"return"`
	HasError     bool     `json:"hasError"`
	Context      bool     `json:"forwardContext"`
	Cancellation string   `json:"cancellation"`
	Provenance   string   `json:"provenance"`
	// HostParameters and HostResults are the complete native signature with
	// each component's explicit adaptation, including nullability.
	HostParameters []HostComponent `json:"hostParameters"`
	HostResults    []HostComponent `json:"hostResults"`
	// alias and member keep the checked import declaration and exported Go
	// name separately from the alias-qualified Symbol lookup key.
	alias  string
	member string
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
	loader := importer.ForCompiler(gotoken.NewFileSet(), "gc", func(path string) (io.ReadCloser, error) {
		if listed[path].Export == "" {
			return nil, fmt.Errorf("missing export data for %s", path)
		}
		return os.Open(listed[path].Export)
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
	host := &hostImports{aliases: map[string]string{}, types: map[string]*types.TypeName{}, unsupported: map[string]string{}}
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
				b, unsupported, err := normalizeBinding(imp, member, contracts, host)
				if err != nil {
					r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF111", Message: err.Error(), Span: imp.Span})
				} else if unsupported != "" {
					host.unsupported[b.Symbol] = unsupported
				} else {
					r.Program.Bindings[b.Symbol] = b
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
	return contracts, &main, nil
}

// normalizeBinding admits one exported function from its native signature. A
// refused function returns the reason, reported when source calls it.
func normalizeBinding(imp GoImport, fn *types.Func, contracts map[string]behavior, host *hostImports) (Binding, string, error) {
	sig := fn.Type().(*types.Signature)
	b := Binding{Symbol: imp.Alias + "." + fn.Name(), Package: imp.Path, Signature: sig.String(), Cancellation: "unknown", Provenance: "Go export data; behavior unclassified", alias: imp.Alias, member: fn.Name()}
	meta := contracts[imp.Path+"."+fn.Name()]
	if meta.Context != "" && meta.Context != "fiber" {
		return b, "", fmt.Errorf("unsupported context contract for %s", b.Symbol)
	}
	if meta.Cancellation != "" && meta.Cancellation != "cooperative" && meta.Cancellation != "unknown" {
		return b, "", fmt.Errorf("unsupported cancellation contract for %s", b.Symbol)
	}
	if meta.Cancellation != "" {
		b.Cancellation = meta.Cancellation
		b.Provenance = "Go export data; reviewed effra.bindings.json assertion"
	}
	if sig.TypeParams().Len() > 0 {
		return b, "generic Go functions are unsupported", nil
	}
	if sig.Variadic() {
		return b, "variadic Go functions are unsupported", nil
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
			return b, fmt.Sprintf("parameter %d: %v", i+1, err), nil
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
			return b, fmt.Sprintf("result %d: %v", i+1, err), nil
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
		return b, "the context contract requires a first context.Context parameter", nil
	}
	if meta.Cancellation == "cooperative" && !b.Context {
		return b, "cooperative cancellation requires context forwarding", nil
	}
	return b, "", nil
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
		b, ok := c.program.Bindings[key]
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
		if len(e.Args) != len(host.params) {
			c.diagnostic("EF106", "incorrect Go argument count for "+key, e.Span)
		}
		for i, arg := range e.Args {
			actual := c.expr(arg, env, inEffect)
			if i >= len(host.params) || c.hostIntegerLiteral(arg, host.params[i]) {
				continue
			}
			if actual.isEffect() || !c.assignable(actual.valueID(), host.params[i], 0) {
				c.diagnostic("EF106", "Go argument must be "+c.displayTypeID(host.params[i]), arg.Span)
			}
		}
		t := checkedExpression{value: c.values.recipe(host.result, nil, checkedEffectCallable, emptyRowID, c.internRow([]string{"Foreign"}), nil, nil)}
		e.checked = t.clone()
		e.Type = c.projectChecked(t)
		e.Text = "foreign"
		e.Name = key
		c.program.UsedImports[alias] = true
		found := false
		for _, existing := range c.result.Bindings {
			if existing.Symbol == key {
				found = true
			}
		}
		if !found {
			c.result.Bindings = append(c.result.Bindings, b)
		}
		return true
	}
	return false
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
