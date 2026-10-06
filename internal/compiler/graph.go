package compiler

import (
	"fmt"
	"slices"
)

type GraphNode struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"`
	Name     string     `json:"name"`
	Span     Span       `json:"span"`
	Contract *ValueType `json:"contract,omitempty"`
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

// Graph describes lexical construction and declared dependency edges. It is not
// a runtime allocation graph or a claim about execution order or layer sharing.
func (r *Result) Graph() (*DependencyGraph, error) {
	if !r.Checked {
		return nil, fmt.Errorf("dependency graphs require checked source")
	}
	g := &DependencyGraph{SchemaVersion: r.SchemaVersion, Revision: r.Revision, Target: r.Target, Nodes: []GraphNode{}, Edges: []GraphEdge{}, Limitations: []string{"single-file static graph; includes deferred recipe construction, not execution order", "provider construction is pure and non-memoized; fallible acquisition, sharing, lifecycle-safe capture and cycle solving are not implemented", "node IDs containing offsets are scoped to the semantic revision"}}
	nodes := map[string]bool{}
	add := func(id, kind, name string, span Span, contract *ValueType) {
		if !nodes[id] {
			nodes[id] = true
			g.Nodes = append(g.Nodes, GraphNode{id, kind, name, span, contract})
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
	var block func(*Block, string)
	var expr func(*Expr, string) string
	expr = func(e *Expr, owner string) string {
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
		left := expr(e.Left, id)
		expr(e.Right, id)
		for _, a := range e.Args {
			expr(a, id)
		}
		for _, field := range e.Fields {
			expr(field.Value, id)
		}
		for _, arm := range e.Arms {
			block(arm.Body, id)
		}
		block(e.Then, id)
		block(e.Else, id)
		if e.Kind == "provide" {
			edge(id, left, "adapts", "", e.Span)
			provider := "provider:" + e.Right.Name
			if !nodes[provider] {
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
	block = func(b *Block, owner string) {
		if b != nil {
			for _, s := range b.Statements {
				expr(s.Value, owner)
				expr(s.Payload, owner)
			}
		}
	}
	for _, f := range r.Program.Functions {
		block(f.Body, "function:"+f.Name)
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
			block(f.Body, id)
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
	return g, nil
}
