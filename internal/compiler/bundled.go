package compiler

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"slices"
)

// This index is compiler-distributed. It never resolves paths through the
// filesystem, Go importer, network or an untrusted user interface file.
//
//go:embed bundled/functions/*.ef
var bundledSources embed.FS

type bundledDeclaration struct {
	Source       string
	Dependencies []string
}

var bundledIndex = map[string]map[string]bundledDeclaration{
	"effra/functions": {"call": {Source: "bundled/functions/call.ef"}, "identity": {Source: "bundled/functions/identity.ef"}},
}

const bundledInterfaceVersion = "1"
const SemanticProducerIdentity = "effra/checker-abi-5/bundled-source-1"
const maxBundledDeclarations = 256
const maxBundledReferences = 4096
const maxBundledSourceBytes = 1 << 20

type BundledImport struct {
	Alias string `json:"alias"`
	Path  string `json:"path"`
	Span  Span   `json:"span"`
}

// SourceInfo identifies offsets independently of the caller's import alias.
// Source text remains separately parsed; there is no offset-shifting prelude.
type SourceInfo struct {
	ID      string `json:"id"`
	Module  string `json:"module"`
	Digest  string `json:"digest"`
	Version string `json:"version,omitempty"`
}

type BundledBinding struct {
	Module      string `json:"module"`
	Name        string `json:"name"`
	Declaration string `json:"declaration"`
	Source      string `json:"source"`
	Span        Span   `json:"span"`
	Content     string `json:"content"`
}

func (f *Function) goEmissionName() string {
	if f.EmissionName != "" {
		return "efBundledFunction_" + f.EmissionName
	}
	return "efFunction_" + f.Name
}

func (f *Function) jsEmissionName() string {
	if f.EmissionName != "" {
		return "__ef_bundled_function_" + f.EmissionName
	}
	return "__ef_function_" + f.Name
}

func (p *Program) checkedFunctions() []*Function {
	return append(append([]*Function{}, p.Functions...), p.BundledFunctions...)
}

func (p *Program) bundledFunction(e *Expr) *Function {
	if e == nil || e.Kind != "member" || e.Left == nil || e.Left.Kind != "name" {
		return nil
	}
	return p.BundledBindings[e.Left.Name][e.Name]
}

func (c *checker) namedFunction(name string) *Function {
	if c.functionModule == "" || c.functionModule == currentModuleIdentity {
		return c.functions[name]
	}
	for _, f := range c.program.BundledFunctions {
		if f.Module == c.functionModule && f.Name == name {
			return f
		}
	}
	return nil
}

// AddSourceInputs supplies the same bounded source provenance to CLI/MCP
// projections. Their existing complete-response byte checks include it.
func (r *Result) AddSourceInputs(response map[string]any) {
	response["producerIdentity"] = r.ProducerIdentity
	response["sources"] = append([]SourceInfo{}, r.Sources...)
	response["bundledBindings"] = append([]BundledBinding{}, r.BundledBindings...)
}

// loadBundledImports uses a bounded declaration queue, with aliases as lookup
// bindings only. Each admitted body enters the ordinary checker; interface
// availability by itself does not admit or emit a function body.
func (r *Result) loadBundledImports(source string) {
	p := r.Program
	p.BundledBindings = map[string]map[string]*Function{}
	r.Sources = []SourceInfo{{ID: "source:user", Module: currentModuleIdentity, Digest: formatDigest(source)}}
	r.ProducerIdentity = SemanticProducerIdentity
	aliases := map[string]string{}
	for _, imp := range p.BundledImports {
		if _, exists := bundledIndex[imp.Path]; !exists {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{"EF126", "unsupported bundled module " + imp.Path + "; user package resolution is unavailable", imp.Span})
			continue
		}
		aliases[imp.Alias] = imp.Path
		p.BundledBindings[imp.Alias] = map[string]*Function{}
	}
	type request struct {
		module, member string
		span           Span
	}
	queue := []request{}
	exhausted := false
	var visit func(*Expr, map[string]bool)
	var block func(*Block, map[string]bool)
	visit = func(e *Expr, locals map[string]bool) {
		if e == nil {
			return
		}
		if e.Kind == "member" && e.Left.Kind == "name" && !locals[e.Left.Name] {
			if module := aliases[e.Left.Name]; module != "" {
				if len(queue) >= maxBundledReferences {
					exhausted = true
				} else {
					queue = append(queue, request{module, e.Name, e.Span})
				}
			}
		}
		forEachExprChild(e, func(child *Expr) { visit(child, locals) })
		block(e.Then, locals)
		block(e.Else, locals)
		for _, arm := range e.Arms {
			bound := cloneBoundNames(locals)
			if arm.Pattern != nil {
				for _, name := range arm.Pattern.Bindings {
					bound[name] = true
				}
			}
			block(arm.Body, bound)
		}
	}
	block = func(b *Block, locals map[string]bool) {
		if b == nil {
			return
		}
		bound := cloneBoundNames(locals)
		for _, s := range b.Statements {
			visit(s.Value, bound)
			visit(s.Payload, bound)
			if s.Kind == "let" {
				bound[s.Name] = true
			}
		}
	}
	root := func(f *Function, extras []Param) {
		bound := map[string]bool{}
		for _, param := range append(append([]Param{}, f.Params...), extras...) {
			bound[param.Name] = true
		}
		block(f.Body, bound)
	}
	for _, f := range p.Functions {
		root(f, nil)
	}
	for _, provider := range p.Providers {
		for _, f := range provider.Methods {
			root(f, provider.Params)
		}
	}
	loaded := map[string]*Function{}
	if exhausted {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{"EF126", "bundled reference admission exceeds budget", Span{}})
		return
	}
	bytes := 0
	for head := 0; head < len(queue); head++ {
		req := queue[head]
		key := req.module + "/" + req.member
		if loaded[key] != nil {
			continue
		}
		entry := bundledIndex[req.module][req.member]
		if entry.Source == "" {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{"EF126", "unknown bundled declaration " + key, req.span})
			continue
		}
		data, err := bundledSources.ReadFile(entry.Source)
		bytes += len(data)
		if err != nil || bytes > maxBundledSourceBytes || len(loaded) >= maxBundledDeclarations {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{"EF126", "bundled source admission unavailable or exceeds budget", req.span})
			return
		}
		bundle, diagnostics := parse(string(data))
		if len(diagnostics) != 0 || bundle == nil || len(bundle.Functions) != 1 || len(bundle.Items) != 1 || bundle.Functions[0].Name != req.member {
			r.Diagnostics = append(r.Diagnostics, Diagnostic{"EF126", "invalid distributed function source " + key, req.span})
			return
		}
		f := bundle.Functions[0]
		f.Module = req.module
		f.SourceID = "source:" + key
		f.Identity = "function:" + req.module + ":module:" + f.Name
		sum := sha256.Sum256([]byte(f.Identity))
		f.EmissionName = "bundle_" + hex.EncodeToString(sum[:8])
		loaded[key] = f
		for _, dependency := range entry.Dependencies {
			if len(queue) >= maxBundledReferences {
				r.Diagnostics = append(r.Diagnostics, Diagnostic{"EF126", "bundled declaration closure exceeds budget", req.span})
				return
			}
			queue = append(queue, request{req.module, dependency, req.span})
		}
		p.BundledFunctions = append(p.BundledFunctions, f)
		for name, typ := range bundle.typeExpressions {
			p.typeExpressions[name] = typ
		}
		content := formatDigest(string(data))
		r.Sources = append(r.Sources, SourceInfo{ID: f.SourceID, Module: req.module, Digest: content, Version: bundledInterfaceVersion})
		r.BundledBindings = append(r.BundledBindings, BundledBinding{Module: req.module, Name: f.Name, Declaration: f.Identity, Source: f.SourceID, Span: f.DeclSpan, Content: content})
		// Dependencies use checked module members, rather than arbitrary source
		// lookup. The first distribution has self-contained ordinary helpers;
		// unsupported names are diagnosed by the common checker below.
		for alias, module := range aliases {
			if module == req.module {
				p.BundledBindings[alias][f.Name] = f
			}
		}
	}
	slices.SortFunc(r.BundledBindings, func(a, b BundledBinding) int { return compareStrings(a.Declaration, b.Declaration) })
	slices.SortFunc(r.Sources, func(a, b SourceInfo) int { return compareStrings(a.ID, b.ID) })
	if len(r.BundledBindings) > 0 {
		h := sha256.New()
		fmt.Fprint(h, r.Revision, "\x00", r.Target)
		for _, b := range r.BundledBindings {
			fmt.Fprint(h, "\x00", b.Declaration, "\x00", b.Content, "\x00", bundledInterfaceVersion)
		}
		r.Revision = hex.EncodeToString(h.Sum(nil))
	}
}

func cloneBoundNames(locals map[string]bool) map[string]bool {
	copy := make(map[string]bool, len(locals))
	for name, value := range locals {
		copy[name] = value
	}
	return copy
}

func compareStrings(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
