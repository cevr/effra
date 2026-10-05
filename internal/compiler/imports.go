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
	"os"
	"os/exec"
	"path/filepath"
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
	ImportPath, Export string
	Module             *goModule
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
	exports := map[string]string{}
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
		exports[pkg.ImportPath] = pkg.Export
		if pkg.Module != nil {
			modules[pkg.Module.Path] = pkg.Module
		}
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
	for _, imp := range r.Program.Imports {
		pkg, err := loader.Import(imp.Path)
		if err != nil {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF111", Message: err.Error(), Span: imp.Span})
			continue
		}
		archive, err := os.ReadFile(exports[imp.Path])
		if err != nil {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF111", Message: err.Error(), Span: imp.Span})
			continue
		}
		hash.Write([]byte(imp.Path))
		hash.Write(archive)
		for _, name := range pkg.Scope().Names() {
			fn, ok := pkg.Scope().Lookup(name).(*types.Func)
			if !ok || !fn.Exported() {
				continue
			}
			b, supported, err := normalizeBinding(imp, fn, contracts)
			if err != nil {
				r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF111", Message: err.Error(), Span: imp.Span})
			} else if supported {
				r.Program.Bindings[b.Symbol] = b
			}
		}
	}
	r.Revision = hex.EncodeToString(hash.Sum(nil))
	for _, module := range modules {
		r.Program.Modules = append(r.Program.Modules, module)
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

func normalizeBinding(imp GoImport, fn *types.Func, contracts map[string]behavior) (Binding, bool, error) {
	sig := fn.Type().(*types.Signature)
	b := Binding{Symbol: imp.Alias + "." + fn.Name(), Package: imp.Path, Signature: sig.String(), Cancellation: "unknown", Provenance: "Go export data; behavior unclassified"}
	meta := contracts[imp.Path+"."+fn.Name()]
	if meta.Context != "" && meta.Context != "fiber" {
		return b, false, fmt.Errorf("unsupported context contract for %s", b.Symbol)
	}
	if meta.Cancellation != "" && meta.Cancellation != "cooperative" && meta.Cancellation != "unknown" {
		return b, false, fmt.Errorf("unsupported cancellation contract for %s", b.Symbol)
	}
	if meta.Cancellation != "" {
		b.Cancellation = meta.Cancellation
		b.Provenance = "Go export data; reviewed effra.bindings.json assertion"
	}
	supported := !sig.Variadic() && sig.TypeParams().Len() == 0
	for i := 0; i < sig.Params().Len(); i++ {
		typ := sig.Params().At(i).Type()
		if i == 0 && isContext(typ) && meta.Context == "fiber" {
			b.Context = true
			b.Provenance = "Go export data; reviewed effra.bindings.json assertion"
			continue
		}
		mapped := hostType(typ)
		supported = supported && mapped != ""
		b.Params = append(b.Params, mapped)
	}
	results := sig.Results()
	b.Return = "()"
	if results.Len() == 1 && types.Identical(results.At(0).Type(), types.Universe.Lookup("error").Type()) {
		b.HasError = true
	}
	if results.Len() > 0 && !b.HasError {
		b.Return = hostType(results.At(0).Type())
		supported = supported && b.Return != ""
	}
	if results.Len() == 2 && types.Identical(results.At(1).Type(), types.Universe.Lookup("error").Type()) {
		b.HasError = true
	} else if results.Len() > 1 {
		supported = false
	}
	if meta.Context == "fiber" && !b.Context {
		supported = false
	}
	if meta.Cancellation == "cooperative" && !b.Context {
		supported = false
	}
	return b, supported, nil
}

func isContext(t types.Type) bool {
	named, ok := types.Unalias(t).(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "context" && named.Obj().Name() == "Context"
}

func hostType(t types.Type) string {
	t = types.Unalias(t)
	if basic, ok := t.(*types.Basic); ok {
		switch basic.Kind() {
		case types.String:
			return "string"
		case types.Bool:
			return "bool"
		case types.Int64:
			return "i64"
		}
	}
	if slice, ok := t.(*types.Slice); ok && types.Identical(slice.Elem(), types.Typ[types.Uint8]) {
		return "bytes"
	}
	return ""
}

func (c *checker) foreignCall(e *Expr, env map[string]ValueType, inEffect bool) bool {
	if e.Left.Kind != "member" || e.Left.Left.Kind != "name" {
		return false
	}
	alias := e.Left.Left.Name
	for _, imp := range c.program.Imports {
		if imp.Alias != alias {
			continue
		}
		if _, shadow := env[alias]; shadow {
			c.diagnostic("EF103", "local shadows Go import "+alias, e.Span)
			return true
		}
		key := alias + "." + e.Left.Name
		b, ok := c.program.Bindings[key]
		if !ok {
			c.diagnostic("EF112", "unknown or unsupported Go symbol "+key+"; supports non-generic primitive functions and optional error returns", e.Span)
			e.Type = value("invalid")
			return true
		}
		if len(e.Args) != len(b.Params) {
			c.diagnostic("EF106", "incorrect Go argument count for "+key, e.Span)
		}
		for i, arg := range e.Args {
			actual := c.expr(arg, env, inEffect)
			if i < len(b.Params) && (actual.Effect || actual.Success != b.Params[i]) {
				c.diagnostic("EF106", "Go argument must be "+b.Params[i], arg.Span)
			}
		}
		t := value(b.Return)
		if b.HasError {
			t.Success = "GoResult:" + b.Return
		}
		t.Effect = true
		t.Services = []string{"Foreign"}
		e.Type = t
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
	out.WriteString("module effra.generated\n\ngo 1.27\n")
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
