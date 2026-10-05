package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
)

type ValueType struct {
	Success  string   `json:"success"`
	Effect   bool     `json:"effect"`
	Errors   []string `json:"failures"`
	Services []string `json:"requirements"`
}
type Contribution struct {
	Kind  string   `json:"kind"`
	Names []string `json:"names"`
	Span  Span     `json:"span"`
}
type Symbol struct {
	Name          string         `json:"name"`
	Params        []Param        `json:"parameters"`
	Contract      ValueType      `json:"contract"`
	Actual        ValueType      `json:"bodyContract"`
	Span          Span           `json:"span"`
	Contributions []Contribution `json:"contributions"`
}
type Timings struct {
	ParseMicros int64 `json:"parseMicros"`
	CheckMicros int64 `json:"checkMicros"`
	TotalMicros int64 `json:"totalMicros"`
}
type Result struct {
	SchemaVersion int          `json:"schemaVersion"`
	Revision      string       `json:"revision"`
	Target        string       `json:"target"`
	Checked       bool         `json:"checked"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
	Symbols       []Symbol     `json:"symbols"`
	Timings       Timings      `json:"timings"`
	Program       *Program     `json:"-"`
}
type checker struct {
	program   *Program
	result    *Result
	functions map[string]*Function
	services  map[string]*Service
	providers map[string]*Provider
	reasons   []Contribution
}

func value(success string) ValueType {
	return ValueType{Success: success, Errors: []string{}, Services: []string{}}
}
func contract(f *Function) ValueType {
	v := value(f.Return)
	v.Effect = f.Effect
	v.Errors = normalized(f.Errors)
	v.Services = normalized(f.Services)
	return v
}
func normalized(names []string) []string {
	out := append([]string{}, names...)
	slices.Sort(out)
	return slices.Compact(out)
}
func union(a, b []string) []string { return normalized(append(append([]string{}, a...), b...)) }
func remove(a []string, n string) []string {
	out := []string{}
	for _, v := range a {
		if v != n {
			out = append(out, v)
		}
	}
	return out
}
func difference(a, b []string) []string {
	out := []string{}
	for _, v := range a {
		if !slices.Contains(b, v) {
			out = append(out, v)
		}
	}
	return out
}
func Compile(source string) *Result { return CompileFor(source, "go") }
func CompileFor(source, target string) *Result {
	start := time.Now()
	hash := sha256.Sum256([]byte(source))
	r := &Result{SchemaVersion: 1, Revision: hex.EncodeToString(hash[:]), Target: target, Diagnostics: []Diagnostic{}, Symbols: []Symbol{}}
	if target != "go" && target != "js" {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: "EF110", Message: "unsupported target " + target})
		return r
	}
	program, diagnostics := parse(source)
	r.Timings.ParseMicros = time.Since(start).Microseconds()
	if len(diagnostics) > 0 {
		r.Diagnostics = diagnostics
		r.Timings.TotalMicros = time.Since(start).Microseconds()
		return r
	}
	r.Program = program
	c := &checker{program: program, result: r, functions: map[string]*Function{}, services: map[string]*Service{}, providers: map[string]*Provider{}}
	checkStart := time.Now()
	c.check()
	r.Timings.CheckMicros = time.Since(checkStart).Microseconds()
	r.Timings.TotalMicros = time.Since(start).Microseconds()
	r.Checked = len(r.Diagnostics) == 0
	return r
}
func (c *checker) diagnostic(code, message string, span Span) {
	c.result.Diagnostics = append(c.result.Diagnostics, Diagnostic{code, message, span})
}
func (c *checker) check() {
	names := map[string]bool{}
	for _, s := range builtins() {
		c.services[s.Name] = s
		names[s.Name] = true
	}
	for _, p := range builtinProviders() {
		c.providers[p.Name] = p
		names[p.Name] = true
	}
	for _, name := range builtinErrors() {
		names[name] = true
	}
	claim := func(name string, span Span) {
		if names[name] {
			c.diagnostic("EF101", "duplicate declaration "+name, span)
		}
		names[name] = true
	}
	errors := make([]string, 0, len(c.program.Errors))
	for name := range c.program.Errors {
		errors = append(errors, name)
	}
	slices.Sort(errors)
	for _, name := range errors {
		claim(name, c.program.Errors[name])
	}
	for _, name := range builtinErrors() {
		c.program.Errors[name] = Span{}
	}
	for _, s := range c.program.Services {
		claim(s.Name, s.Span)
		c.services[s.Name] = s
	}
	for _, p := range c.program.Providers {
		claim(p.Name, p.Span)
		c.providers[p.Name] = p
	}
	for _, f := range c.program.Functions {
		claim(f.Name, f.Span)
		c.functions[f.Name] = f
	}
	for _, s := range c.program.Services {
		methods := map[string]bool{}
		for _, f := range s.Methods {
			if methods[f.Name] {
				c.diagnostic("EF101", "duplicate method "+f.Name, f.Span)
			}
			methods[f.Name] = true
			c.signature(f)
			if len(f.Services) > 0 {
				c.diagnostic("EF103", "service methods cannot declare uses in this prototype", f.Span)
			}
		}
	}
	for _, f := range c.program.Functions {
		c.signature(f)
	}
	for _, p := range c.program.Providers {
		s, exists := c.services[p.Service]
		if !exists {
			c.diagnostic("EF102", "unknown service "+p.Service, p.Span)
			continue
		}
		methods := map[string]*Function{}
		for _, f := range p.Methods {
			if methods[f.Name] != nil {
				c.diagnostic("EF101", "duplicate implementation method "+f.Name, f.Span)
			}
			methods[f.Name] = f
			c.signature(f)
			c.function(f, false)
			if len(f.Services) > 0 {
				c.diagnostic("EF103", "providers must be self-contained in this prototype", f.Span)
			}
		}
		for _, want := range s.Methods {
			got := methods[want.Name]
			if got == nil {
				c.diagnostic("EF104", "provider "+p.Name+" is missing method "+want.Name, p.Span)
				continue
			}
			equal := got.Effect && got.Return == want.Return && len(got.Params) == len(want.Params)
			if equal {
				for i := range got.Params {
					equal = equal && got.Params[i].Type == want.Params[i].Type
				}
			}
			if !equal || len(difference(got.Errors, want.Errors)) > 0 {
				c.diagnostic("EF104", "implementation does not satisfy service method "+s.Name+"."+want.Name, got.Span)
			}
		}
		for _, f := range p.Methods {
			found := false
			for _, want := range s.Methods {
				found = found || want.Name == f.Name
			}
			if !found {
				c.diagnostic("EF104", "unexpected implementation method "+f.Name, f.Span)
			}
		}
	}
	for _, f := range c.program.Functions {
		c.function(f, true)
	}
}
func (c *checker) signature(f *Function) {
	valid := func(t string, span Span) {
		if t != "string" && t != "bool" && t != "()" && t != "i64" && t != "File" && t != "bytes" {
			c.diagnostic("EF102", "unsupported value type "+t+"; prototype supports string, bool, i64, bytes, File, ()", span)
		}
	}
	valid(f.Return, f.Span)
	names := map[string]bool{}
	for _, p := range f.Params {
		valid(p.Type, p.Span)
		if names[p.Name] {
			c.diagnostic("EF101", "duplicate parameter "+p.Name, p.Span)
		}
		names[p.Name] = true
	}
	for _, name := range normalized(f.Errors) {
		if _, exists := c.program.Errors[name]; !exists {
			c.diagnostic("EF102", "unknown failure "+name, f.Span)
		}
	}
	for _, name := range normalized(f.Services) {
		if _, exists := c.services[name]; !exists {
			c.diagnostic("EF102", "unknown service "+name, f.Span)
		}
	}
	if !f.Effect && (len(f.Errors) > 0 || len(f.Services) > 0) {
		c.diagnostic("EF103", "ordinary functions cannot declare effect rows", f.Span)
	}
}
func (c *checker) function(f *Function, record bool) {
	env := map[string]ValueType{}
	for _, p := range f.Params {
		env[p.Name] = value(p.Type)
	}
	c.reasons = []Contribution{}
	actual := c.block(f.Body, env, f.Effect)
	if actual.Success != "never" && (actual.Success != f.Return || actual.Effect) {
		c.diagnostic("EF106", fmt.Sprintf("body returns %s; expected %s", display(actual), f.Return), f.Span)
	}
	if missing := difference(actual.Errors, f.Errors); len(missing) > 0 {
		c.diagnostic("EF107", "undeclared failures: "+strings.Join(missing, ", "), f.Span)
	}
	if missing := difference(actual.Services, f.Services); len(missing) > 0 {
		c.diagnostic("EF108", "missing service requirements: "+strings.Join(missing, ", "), f.Span)
	}
	actual.Effect = f.Effect
	if record {
		c.result.Symbols = append(c.result.Symbols, Symbol{f.Name, f.Params, contract(f), actual, f.Span, append([]Contribution{}, c.reasons...)})
	}
}
func display(t ValueType) string {
	if t.Effect {
		return "Effect<" + t.Success + ", {" + strings.Join(t.Errors, ", ") + "}, {" + strings.Join(t.Services, ", ") + "}>"
	}
	return t.Success
}
func clone(env map[string]ValueType) map[string]ValueType {
	copy := map[string]ValueType{}
	for n, t := range env {
		copy[n] = t
	}
	return copy
}
func (c *checker) block(b *Block, env map[string]ValueType, effect bool) ValueType {
	out := value("()")
	env = clone(env)
	terminated := false
	for _, s := range b.Statements {
		if terminated {
			c.diagnostic("EF109", "unreachable statement after fail", s.Span)
		}
		if s.Kind == "fail" {
			if !effect {
				c.diagnostic("EF105", "fail is only valid inside effect functions", s.Span)
			}
			if _, exists := c.program.Errors[s.Name]; !exists {
				c.diagnostic("EF102", "unknown failure "+s.Name, s.Span)
			}
			out.Errors = union(out.Errors, []string{s.Name})
			out.Success = "never"
			out.Effect = false
			terminated = true
			c.reasons = append(c.reasons, Contribution{"failure", []string{s.Name}, s.Span})
			continue
		}
		t := c.expr(s.Value, env, effect)
		out.Errors = union(out.Errors, tExecutedErrors(s.Value))
		out.Services = union(out.Services, tExecutedServices(s.Value))
		if s.Kind == "let" {
			if _, exists := env[s.Name]; exists {
				c.diagnostic("EF101", "duplicate local "+s.Name, s.Span)
			}
			env[s.Name] = t
			out.Success = "()"
			out.Effect = false
		} else {
			if t.Effect {
				c.diagnostic("EF105", "unused lazy effect; execute with run or bind it with let", s.Span)
			}
			out.Success = t.Success
			out.Effect = t.Effect
		}
	}
	return out
}

// Effect values carry deferred rows. Only run (and executed branch bodies) contribute to the enclosing computation.
func tExecutedErrors(e *Expr) []string   { return executed(e, true) }
func tExecutedServices(e *Expr) []string { return executed(e, false) }
func executed(e *Expr, errors bool) []string {
	if e == nil {
		return []string{}
	}
	row := func(t ValueType) []string {
		if errors {
			return t.Errors
		}
		return t.Services
	}
	if e.Kind == "run" || e.Kind == "if" || e.Kind == "scope" || e.Kind == "fork" {
		return row(e.Type)
	}
	out := union(executed(e.Left, errors), executed(e.Right, errors))
	for _, a := range e.Args {
		out = union(out, executed(a, errors))
	}
	return out
}
func (c *checker) expr(e *Expr, env map[string]ValueType, inEffect bool) ValueType {
	t := value("invalid")
	switch e.Kind {
	case "integer":
		t = value("i64")
	case "string":
		t = value("string")
	case "bool":
		t = value("bool")
	case "unit":
		t = value("()")
	case "name":
		if v, exists := env[e.Name]; exists {
			t = v
			e.Text = "local"
		} else if p, exists := c.providers[e.Name]; exists {
			t = value("provider:" + p.Service)
			if p.Service == "Files" || p.Service == "Runtime" || p.Service == "Foreign" {
				c.requireGo(e.Span, "native provider "+p.Service)
			}
			e.Text = "provider"
		} else {
			c.diagnostic("EF102", "unknown value "+e.Name, e.Span)
		}
	case "call":
		if c.fiberCall(e, env, inEffect) {
			t = e.Type
			break
		}
		var f *Function
		if e.Left.Kind == "name" {
			f = c.functions[e.Left.Name]
			if _, shadow := env[e.Left.Name]; shadow {
				c.diagnostic("EF103", "calling local values is not supported in this prototype", e.Span)
				f = nil
			}
		} else if e.Left.Kind == "member" && e.Left.Left.Kind == "name" {
			key := e.Left.Left.Name
			if key == "Files" || key == "Runtime" {
				c.requireGo(e.Span, "native service "+key)
			}
			if _, shadow := env[key]; shadow {
				c.diagnostic("EF103", "a local shadows service "+key, e.Span)
			} else if s := c.services[key]; s != nil {
				for _, m := range s.Methods {
					if m.Name == e.Left.Name {
						f = m
						t.Services = []string{key}
						break
					}
				}
			}
		}
		if f == nil {
			c.diagnostic("EF102", "unknown function or service method", e.Span)
			for _, a := range e.Args {
				c.expr(a, env, inEffect)
			}
			break
		}
		services := t.Services
		t = contract(f)
		t.Services = union(t.Services, services)
		if len(e.Args) != len(f.Params) {
			c.diagnostic("EF106", "incorrect argument count", e.Span)
		}
		for i, a := range e.Args {
			arg := c.expr(a, env, inEffect)
			if i < len(f.Params) && (arg.Effect || arg.Success != f.Params[i].Type) {
				c.diagnostic("EF106", "argument must be "+f.Params[i].Type, a.Span)
			}
		}
	case "scope":
		c.requireGo(e.Span, "scopes")
		if !inEffect {
			c.diagnostic("EF105", "scope requires an effect function", e.Span)
		}
		t = c.block(e.Then, clone(env), inEffect)
	case "fork":
		c.requireGo(e.Span, "owned fibers")
		inner := c.expr(e.Left, env, inEffect)
		if !inEffect || !inner.Effect {
			c.diagnostic("EF105", "fork requires an Effect inside an effect function", e.Span)
		}
		t = inner
		t.Success = "Fiber:" + inner.Success
		t.Effect = false
		t.Errors = union(t.Errors, executed(e.Left, true))
		t.Services = union(t.Services, executed(e.Left, false))
		c.reasons = append(c.reasons, Contribution{"owned-child", inner.Errors, e.Span})
	case "timeout":
		c.requireGo(e.Span, "managed timeout")
		t = c.expr(e.Left, env, inEffect)
		duration := c.expr(e.Right, env, inEffect)
		if !t.Effect || duration.Effect || duration.Success != "i64" {
			c.diagnostic("EF106", "timeout requires an Effect and an i64 millisecond duration", e.Span)
		}
		t.Errors = union(t.Errors, []string{"Timeout"})
	case "run":
		inner := c.expr(e.Left, env, inEffect)
		if !inEffect {
			c.diagnostic("EF105", "run is only valid inside effect functions", e.Span)
		}
		if !inner.Effect {
			c.diagnostic("EF105", "run requires an Effect value", e.Span)
		}
		t = inner
		t.Effect = false
		t.Errors = union(t.Errors, executed(e.Left, true))
		t.Services = union(t.Services, executed(e.Left, false))
		if len(t.Errors) > 0 {
			c.reasons = append(c.reasons, Contribution{"failure", t.Errors, e.Span})
		}
		if len(t.Services) > 0 {
			c.reasons = append(c.reasons, Contribution{"requirement", t.Services, e.Span})
		}
	case "provide":
		t = c.expr(e.Left, env, inEffect)
		provider := c.expr(e.Right, env, inEffect)
		if !t.Effect {
			c.diagnostic("EF105", "provide requires an Effect value", e.Span)
		}
		if c.services[e.Name] == nil {
			c.diagnostic("EF102", "unknown service "+e.Name, e.Span)
		}
		if provider.Success != "provider:"+e.Name || provider.Effect {
			c.diagnostic("EF104", "provider must implement "+e.Name, e.Right.Span)
		}
		t.Services = remove(t.Services, e.Name)
	case "catch":
		t = c.expr(e.Left, env, inEffect)
		fallback := c.expr(e.Right, env, false)
		if !t.Effect {
			c.diagnostic("EF105", "catch requires an Effect value", e.Span)
		}
		if _, exists := c.program.Errors[e.Name]; !exists {
			c.diagnostic("EF102", "unknown failure "+e.Name, e.Span)
		} else if !slices.Contains(t.Errors, e.Name) {
			c.diagnostic("EF107", "effect does not admit failure "+e.Name, e.Span)
		}
		if fallback.Effect || fallback.Success != t.Success {
			c.diagnostic("EF106", "prototype catch fallback must be a pure "+t.Success, e.Right.Span)
		}
		t.Errors = remove(t.Errors, e.Name)
	case "binary":
		left, right := c.expr(e.Left, env, inEffect), c.expr(e.Right, env, inEffect)
		if left.Effect || right.Effect || left.Success != right.Success || (left.Success != "string" && left.Success != "bool" && left.Success != "i64") || (e.Name == "+" && left.Success != "string") {
			c.diagnostic("EF106", "operator requires matching primitive values; + accepts strings", e.Span)
		}
		t = value(left.Success)
		if e.Name == "==" {
			t.Success = "bool"
		}
	case "if":
		condition := c.expr(e.Left, env, inEffect)
		if condition.Effect || condition.Success != "bool" {
			c.diagnostic("EF106", "if condition must be bool", e.Left.Span)
		}
		a, b := c.block(e.Then, env, inEffect), c.block(e.Else, env, inEffect)
		if a.Success == "never" {
			t = b
		} else if b.Success == "never" {
			t = a
		} else {
			t = a
			if a.Success != b.Success || a.Effect != b.Effect {
				c.diagnostic("EF106", "if branches must return the same type", e.Span)
			}
		}
		if a.Effect || b.Effect {
			c.diagnostic("EF103", "returning Effect values from branches is not supported in this prototype", e.Span)
		}
		t.Errors = union(union(a.Errors, b.Errors), executed(e.Left, true))
		t.Services = union(union(a.Services, b.Services), executed(e.Left, false))
		t.Effect = false
	default:
		c.diagnostic("EF103", "unsupported expression "+e.Kind, e.Span)
	}
	e.Type = t
	return t
}
func (r *Result) Find(name string) *Symbol {
	for i := range r.Symbols {
		if r.Symbols[i].Name == name {
			return &r.Symbols[i]
		}
	}
	return nil
}
func (r *Result) Entry() error {
	if !r.Checked {
		return fmt.Errorf("source has diagnostics")
	}
	main := r.Find("main")
	if main == nil {
		return fmt.Errorf("entry requires an effect fn main")
	}
	if !main.Contract.Effect || len(main.Params) > 0 {
		return fmt.Errorf("main must be an effect function with no parameters")
	}
	if len(main.Contract.Services) > 0 {
		return fmt.Errorf("main has unprovided services: %s", strings.Join(main.Contract.Services, ", "))
	}
	return nil
}

func (c *checker) fiberCall(e *Expr, env map[string]ValueType, inEffect bool) bool {
	if e.Left.Kind != "member" || e.Left.Left.Kind != "name" {
		return false
	}
	inner, exists := env[e.Left.Left.Name]
	if !exists || !strings.HasPrefix(inner.Success, "Fiber:") {
		return false
	}
	if len(e.Args) != 0 {
		c.diagnostic("EF106", "fiber operations take no arguments", e.Span)
	}
	t := value(strings.TrimPrefix(inner.Success, "Fiber:"))
	t.Effect = true
	t.Errors = inner.Errors
	switch e.Left.Name {
	case "join":
	case "interrupt":
		t.Success = "()"
	case "cancel":
		t.Success = "()"
		t.Errors = []string{}
	default:
		c.diagnostic("EF102", "unknown fiber operation "+e.Left.Name, e.Span)
	}
	e.Text = "fiber"
	e.Type = t
	return true
}

func (c *checker) requireGo(span Span, feature string) {
	c.program.GoOnly = true
	if c.result.Target != "go" {
		c.diagnostic("EF110", feature+" is currently implemented only for Go", span)
	}
}
