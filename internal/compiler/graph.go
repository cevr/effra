package compiler

import (
	"fmt"
	"slices"
	"strings"
)

type GraphNode struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"`
	Name     string     `json:"name"`
	Span     Span       `json:"span"`
	Contract *ValueType `json:"contract,omitempty"`
	Incoming []string   `json:"incoming,omitempty"`
}
type GraphEdge struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Kind    string `json:"kind"`
	Service string `json:"service,omitempty"`
	Span    Span   `json:"span"`
}
type DependencyGraph struct {
	SchemaVersion int         `json:"schemaVersion"`
	Revision      string      `json:"revision"`
	Target        string      `json:"target"`
	Nodes         []GraphNode `json:"nodes"`
	Edges         []GraphEdge `json:"edges"`
	Limitations   []string    `json:"limitations"`
}

type providerBinding struct {
	id     string
	recipe bool
}

// Graph describes lexical construction and declared dependency edges. It is not
// a runtime allocation graph or a claim about execution order or layer sharing.
func (r *Result) Graph() (*DependencyGraph, error) {
	if !r.Checked {
		return nil, fmt.Errorf("dependency graphs require checked source")
	}
	g := &DependencyGraph{SchemaVersion: r.SchemaVersion, Revision: r.Revision, Target: r.Target, Nodes: []GraphNode{}, Edges: []GraphEdge{}, Limitations: []string{"single-file static graph; includes deferred recipe construction, not execution order", "provider recipes and explicit value identities are represented without general memoized acquisition; fallible acquisition, lifecycle-safe arbitrary capture and cycle solving are not implemented", "node IDs containing offsets are scoped to the semantic revision"}}
	nodes := map[string]bool{}
	add := func(id, kind, name string, span Span, contract *ValueType) {
		if !nodes[id] {
			nodes[id] = true
			g.Nodes = append(g.Nodes, GraphNode{ID: id, Kind: kind, Name: name, Span: span, Contract: contract})
		}
	}
	edge := func(from, to, kind, service string, span Span) {
		g.Edges = append(g.Edges, GraphEdge{from, to, kind, service, span})
	}
	services := append(append([]*Service{}, builtins()...), r.Program.Services...)
	for _, s := range services {
		add("service:"+s.Name, "service", s.Name, s.Span, nil)
	}
	providers := append(append([]*Provider{}, builtinProviders()...), r.Program.Providers...)
	for _, p := range providers {
		var constructor *ValueType
		if providerConstructed(p) {
			contract := providerContract(p)
			constructor = &contract
		}
		add("provider:"+p.Name, "provider", p.Name, p.Span, constructor)
		edge("provider:"+p.Name, "service:"+p.Service, "implements", p.Service, p.Span)
		for _, requirement := range normalized(p.Services) {
			edge("provider:"+p.Name, "service:"+requirement, "requires", requirement, p.Span)
		}
	}
	for _, s := range r.Symbols {
		t := s.Contract
		add("function:"+s.Name, "function", s.Name, s.Span, &t)
		for _, req := range s.Contract.Services {
			edge("function:"+s.Name, "service:"+req, "requires", req, s.Span)
		}
	}
	providerOrigins := map[*Expr]providerBinding{}
	var block func(*Block, string, map[string]providerBinding)
	var expr func(*Expr, string, map[string]providerBinding) string
	expr = func(e *Expr, owner string, locals map[string]providerBinding) string {
		if e == nil {
			return ""
		}
		id := owner
		if e.Type.Effect || e.Kind == "run" || e.Kind == "scope" || e.Kind == "fork" {
			id = fmt.Sprintf("expression:%s:%d:%s", owner, e.Span.Offset, e.Kind)
			t := e.Type
			add(id, e.Kind, e.Name, e.Span, &t)
			edge(owner, id, "contains", "", e.Span)
			for _, req := range e.Type.Services {
				edge(id, "service:"+req, "requires", req, e.Span)
			}
		}
		left := expr(e.Left, id, locals)
		expr(e.Right, id, locals)
		for _, a := range e.Args {
			expr(a, id, locals)
		}
		for _, field := range e.Fields {
			expr(field.Value, id, locals)
		}
		for _, arm := range e.Arms {
			block(arm.Body, id, cloneStringMap(locals))
		}
		block(e.Then, id, cloneStringMap(locals))
		block(e.Else, id, cloneStringMap(locals))
		if e.Kind == "call" && e.Text == "provider-constructor" {
			recipe := fmt.Sprintf("provider-recipe:%s:%d", owner, e.Span.Offset)
			t := e.Type
			add(recipe, "provider-recipe", e.Left.Name, e.Span, &t)
			providerOrigins[e] = providerBinding{id: recipe, recipe: true}
			edge(id, recipe, "constructs", e.Left.Name, e.Span)
			edge(recipe, "provider:"+e.Left.Name, "originates", "", e.Span)
		}
		if e.Kind == "run" && strings.HasPrefix(e.Type.Success, "provider:") {
			if recipe, ok := providerOrigin(e.Left, locals, providerOrigins, nodes); ok && recipe.recipe {
				provider := "provider-value:" + id
				t := e.Type
				// The value materialized by `run` is reusable and has no
				// unresolved row of its own. Each run gets its own identity,
				// even when it executes the same lazy constructor recipe.
				t.Effect = false
				t.Errors = nil
				t.Services = nil
				add(provider, "provider-value", strings.TrimPrefix(e.Type.Success, "provider:"), e.Span, &t)
				providerOrigins[e] = providerBinding{id: provider}
				edge(id, provider, "materializes", strings.TrimPrefix(e.Type.Success, "provider:"), e.Span)
				edge(provider, recipe.id, "originates", "", e.Span)
			}
		}
		if e.Kind == "provide" {
			edge(id, left, "adapts", "", e.Span)
			binding, ok := providerOrigin(e.Right, locals, providerOrigins, nodes)
			provider := binding.id
			if !ok {
				provider = fmt.Sprintf("provider-value:%s:%d", owner, e.Right.Span.Offset)
				t := e.Right.Type
				add(provider, "provider-value", e.Right.Name, e.Right.Span, &t)
			}
			edge(id, provider, "provides", e.Name, e.Span)
		}
		if e.Kind == "call" {
			if e.Left.Kind == "name" && nodes["function:"+e.Left.Name] {
				edge(id, "function:"+e.Left.Name, "calls", "", e.Span)
			}
			if e.Left.Kind == "member" && e.Left.Left.Kind == "name" && nodes["service:"+e.Left.Left.Name] {
				edge(id, "service:"+e.Left.Left.Name, "calls", e.Left.Left.Name, e.Span)
			}
		}
		return id
	}
	block = func(b *Block, owner string, locals map[string]providerBinding) {
		if b != nil {
			for _, s := range b.Statements {
				expr(s.Value, owner, locals)
				expr(s.Payload, owner, locals)
				if s.Kind == "let" {
					if origin, ok := providerOrigin(s.Value, locals, providerOrigins, nodes); ok {
						locals[s.Name] = origin
					} else {
						delete(locals, s.Name)
					}
				}
			}
		}
	}
	for _, f := range r.Program.Functions {
		block(f.Body, "function:"+f.Name, map[string]providerBinding{})
	}
	for _, p := range r.Program.Providers {
		for _, f := range p.Methods {
			id := "provider-method:" + p.Name + "." + f.Name
			t := contract(f)
			if providerConstructed(p) {
				// Captured constructor requirements are overlaid at invocation;
				// they are not part of the service method's public row.
				t.Services = nil
			}
			add(id, "provider-method", p.Name+"."+f.Name, f.Span, &t)
			edge("provider:"+p.Name, id, "contains", "", f.Span)
			block(f.Body, id, map[string]providerBinding{})
		}
	}
	incoming := map[string][]string{}
	for _, relationship := range g.Edges {
		if nodes[relationship.To] {
			incoming[relationship.To] = append(incoming[relationship.To], relationship.From)
		}
	}
	slices.SortFunc(g.Nodes, func(a, b GraphNode) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	for i := range g.Nodes {
		relationships := incoming[g.Nodes[i].ID]
		slices.Sort(relationships)
		g.Nodes[i].Incoming = slices.Compact(relationships)
	}
	return g, nil
}

func cloneStringMap(values map[string]providerBinding) map[string]providerBinding {
	clone := map[string]providerBinding{}
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func providerOrigin(e *Expr, locals map[string]providerBinding, origins map[*Expr]providerBinding, nodes map[string]bool) (providerBinding, bool) {
	if e == nil {
		return providerBinding{}, false
	}
	if origin, ok := origins[e]; ok {
		return origin, true
	}
	switch e.Kind {
	case "name":
		if e.Text == "local" {
			origin, ok := locals[e.Name]
			return origin, ok
		}
		if e.Text == "provider" && nodes["provider:"+e.Name] {
			return providerBinding{id: "provider:" + e.Name}, true
		}
	case "provide", "catch":
		return providerOrigin(e.Left, locals, origins, nodes)
	}
	return providerBinding{}, false
}
