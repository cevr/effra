package compiler

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	rt "effra.local/prototype/runtime/effra"
)

// endpointDerivationCode refuses an endpoint or route-table declaration whose
// options, handler, witnesses or failure mapping do not form a checked
// endpoint. It is the derive refusal code, shared with codec derivation.
const endpointDerivationCode = codecDerivationCode

// endpointMethods are the request methods an endpoint may name.
var endpointMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE"}

// endpointOptions are an endpoint's options in canonical order.
var endpointOptions = []string{"method", "path", "input", "handler", "output", "status"}

// EndpointDeclaration is one typed HTTP endpoint or route table:
//
//	derive placeOrder = Http.endpoint(method: "POST", path: "/orders", input: orderJson, handler: place, output: receiptJson, status: 201) raises { OutOfStock: 409 outOfStockJson, Unknown: 422 }
//	derive api = Http.routes(placeOrder, health)
//
// The checker synthesizes one ordinary effect function of the derive name
// from HttpRequest to HttpReply, so an endpoint or route table is passed to
// Http.listen, called and inspected like any other handler.
type EndpointDeclaration struct {
	Name           string
	Span           Span
	Member         string
	DerivationSpan Span
	Options        []CodecOption
	Failures       []EndpointFailure
	Routes         []CodecWitnessReference

	method, path string
	status       int64
	input        *CodecDeclaration
	output       *CodecDeclaration
	handler      *Function
	mappings     []*endpointMapping
	endpoints    []*EndpointDeclaration
	function     *Function
}

// EndpointFailure maps one named failure to a response status and, when a
// failure witness is named, a JSON body.
type EndpointFailure struct {
	Name       string
	Status     int64
	Codec      string
	Span       Span
	StatusSpan Span
	CodecSpan  Span
}

// endpointMapping is a checked failure mapping with the origins at which the
// failure is reachable.
type endpointMapping struct {
	failure string
	status  int64
	codec   *CodecDeclaration
	origins []EndpointFailureOrigin
}

// EndpointFailureOrigin names one place an endpoint can raise a failure:
// its handler, or a transformation of its input or output witness.
type EndpointFailureOrigin struct {
	Stage    string `json:"stage"`
	Function string `json:"function"`
	Witness  string `json:"witness,omitempty"`
}

// isEndpointDeclaration reports whether the derive declaration at the
// cursor is `name = Http.endpoint(` or `name = Http.routes(`; every other
// derive declaration is a codec derivation.
func (p *parser) isEndpointDeclaration() bool {
	if p.at+5 >= len(p.tokens) {
		return false
	}
	member := p.tokens[p.at+4].text
	return p.tokens[p.at+1].text == "=" && p.tokens[p.at+2].text == "Http" && p.tokens[p.at+3].text == "." && (member == "endpoint" || member == "routes") && p.tokens[p.at+5].text == "("
}

func (p *parser) endpointDeclaration() *EndpointDeclaration {
	name := p.name()
	p.expect("=")
	alias := p.name()
	p.expect(".")
	member := p.memberName()
	endpoint := &EndpointDeclaration{Name: name.text, Span: name.span, Member: member.text, DerivationSpan: alias.span}
	endpoint.DerivationSpan.Length = member.span.Offset + member.span.Length - alias.span.Offset
	p.expect("(")
	if member.text == "routes" {
		for !p.accept(")") {
			route := p.name()
			endpoint.Routes = append(endpoint.Routes, CodecWitnessReference{Name: route.text, Span: route.span})
			if !p.accept(",") {
				p.expect(")")
				break
			}
		}
		return endpoint
	}
	for !p.accept(")") {
		option := p.name()
		p.expect(":")
		value := p.take()
		parsed := CodecOption{Name: option.text, Span: option.span, ValueSpan: value.span}
		switch value.kind {
		case "integer":
			parsed.Value, _ = strconv.ParseInt(value.text, 10, 64)
		case "name":
			parsed.Function = value.text
		case "string":
			_ = json.Unmarshal([]byte(value.text), &parsed.Text)
			parsed.Quoted = true
		default:
			p.fail(value, "endpoint options take strings, integers or declaration names")
		}
		endpoint.Options = append(endpoint.Options, parsed)
		if !p.accept(",") {
			p.expect(")")
			break
		}
	}
	if p.accept("raises") {
		p.expect("{")
		for !p.accept("}") {
			failure := p.name()
			p.expect(":")
			status := p.take()
			if status.kind != "integer" {
				p.fail(status, "a mapped failure takes a response status")
			}
			entry := EndpointFailure{Name: failure.text, Span: failure.span, StatusSpan: status.span}
			entry.Status, _ = strconv.ParseInt(status.text, 10, 64)
			if p.peek().kind == "name" {
				codec := p.name()
				entry.Codec, entry.CodecSpan = codec.text, codec.span
			}
			endpoint.Failures = append(endpoint.Failures, entry)
			if !p.accept(",") {
				p.expect("}")
				break
			}
		}
	}
	return endpoint
}

// deriveEndpoints checks every endpoint and route table and synthesizes its
// handler function. It runs after every function signature is resolved, so
// a handler's and transformation's declared rows are known, and before
// function summaries are prepared.
func (c *checker) deriveEndpoints(claim func(string, Span)) {
	if len(c.program.Endpoints) == 0 {
		return
	}
	witnesses := map[string]*CodecDeclaration{}
	for _, codec := range c.program.Codecs {
		if codec.headerChecked && witnesses[codec.Name] == nil {
			witnesses[codec.Name] = codec
		}
	}
	named := map[string]*EndpointDeclaration{}
	tables := []*EndpointDeclaration{}
	for _, endpoint := range c.program.Endpoints {
		claim(endpoint.Name, endpoint.Span)
		if named[endpoint.Name] != nil || witnesses[endpoint.Name] != nil {
			continue
		}
		named[endpoint.Name] = endpoint
		switch endpoint.Member {
		case "endpoint":
			if c.checkEndpoint(endpoint, witnesses) {
				c.synthesizeEndpoint(endpoint)
			}
		case "routes":
			tables = append(tables, endpoint)
		}
	}
	for _, table := range tables {
		if c.checkRoutes(table, named) {
			c.synthesizeRoutes(table)
		}
	}
}

// checkEndpoint resolves an endpoint's options, handler, witnesses and
// failure mapping, reporting every refusal it can.
func (c *checker) checkEndpoint(endpoint *EndpointDeclaration, witnesses map[string]*CodecDeclaration) bool {
	ok := true
	refuse := func(message string, span Span) {
		c.diagnostic(endpointDerivationCode, "endpoint "+endpoint.Name+" "+message, span)
		ok = false
	}
	options := map[string]*CodecOption{}
	for i := range endpoint.Options {
		option := &endpoint.Options[i]
		if options[option.Name] != nil {
			refuse("option "+option.Name+" is repeated", option.Span)
			continue
		}
		options[option.Name] = option
		kind := map[string]string{"method": "string", "path": "string", "input": "name", "handler": "name", "output": "name", "status": "integer"}[option.Name]
		switch {
		case kind == "":
			refuse("option "+option.Name+" is unknown; supported options are "+strings.Join(endpointOptions, ", "), option.Span)
		case kind == "string" && !option.Quoted, kind == "name" && option.Function == "", kind == "integer" && (option.Quoted || option.Function != ""):
			refuse("option "+option.Name+" takes "+map[string]string{"string": "a string", "name": "a declaration name", "integer": "an integer"}[kind], option.ValueSpan)
			options[option.Name] = nil
		}
	}
	for _, required := range []string{"method", "path", "handler", "status"} {
		if _, seen := options[required]; !seen {
			refuse("requires option "+required, endpoint.Span)
		}
	}
	if option := options["method"]; option != nil {
		if !slices.Contains(endpointMethods, option.Text) {
			refuse("method "+strconv.Quote(option.Text)+" is not one of "+strings.Join(endpointMethods, ", "), option.ValueSpan)
		}
		endpoint.method = option.Text
	}
	if option := options["path"]; option != nil {
		if !endpointStaticPath(option.Text) {
			refuse("path "+strconv.Quote(option.Text)+" must be an exact static path: / followed by letters, digits, -, ., _, ~ and /", option.ValueSpan)
		}
		endpoint.path = option.Text
	}
	witness := func(name string) *CodecDeclaration {
		option := options[name]
		if option == nil {
			return nil
		}
		codec := witnesses[option.Function]
		switch {
		case codec == nil:
			c.diagnostic("EF102", "unknown codec witness "+option.Function+" for endpoint "+endpoint.Name+" option "+name, option.ValueSpan)
			ok = false
		case codec.failure:
			refuse(name+" "+codec.Name+" is a failure witness; it encodes only mapped failure bodies", option.ValueSpan)
		case codec.decode == nil || codec.encode == nil:
			ok = false
		default:
			return codec
		}
		return nil
	}
	endpoint.input, endpoint.output = witness("input"), witness("output")
	if option := options["status"]; option != nil {
		endpoint.status = option.Value
		switch {
		case option.Value < 200 || option.Value > 299:
			refuse("status "+strconv.FormatInt(option.Value, 10)+" must be a 2xx success status", option.ValueSpan)
		case option.Value == 204 && options["output"] != nil:
			refuse("status 204 carries no body; remove output or choose another status", option.ValueSpan)
		}
	}
	if option := options["handler"]; option != nil {
		f := c.functions[option.Function]
		switch {
		case f == nil:
			c.diagnostic("EF102", "unknown function "+option.Function+" for endpoint "+endpoint.Name+" option handler", option.ValueSpan)
			ok = false
		case len(f.TypeParameters) > 0 || len(f.RowParameters) > 0 || f.endpoint != nil:
			refuse("handler "+f.Name+" must be a first-order function without type or row parameters", option.ValueSpan)
		default:
			endpoint.handler = f
			ok = c.checkEndpointHandler(endpoint, options, option.ValueSpan) && ok
		}
	}
	return c.checkEndpointFailures(endpoint, witnesses) && ok
}

// checkEndpointHandler compares the handler's signature with the endpoint's
// witnesses: it takes the input witness's domain type, or nothing, and
// returns the output witness's domain type, or void.
func (c *checker) checkEndpointHandler(endpoint *EndpointDeclaration, options map[string]*CodecOption, span Span) bool {
	f := endpoint.handler
	refuse := func(message string) bool {
		c.diagnostic(endpointDerivationCode, "endpoint "+endpoint.Name+" handler "+f.Name+" "+message, span)
		return false
	}
	if _, named := options["input"]; named {
		if len(f.Params) != 1 {
			return refuse("must take exactly one parameter, the input witness's domain type")
		}
		if endpoint.input != nil && f.Params[0].typeID != endpoint.input.domainID {
			return refuse("takes " + c.displayTypeID(f.Params[0].typeID) + ", not " + endpoint.input.Type + " decoded by " + endpoint.input.Name)
		}
	} else if len(f.Params) != 0 {
		return refuse("takes no parameters on an endpoint without input")
	}
	if _, named := options["output"]; named {
		if endpoint.output != nil && f.returnID != endpoint.output.domainID {
			return refuse("returns " + c.displayTypeID(f.returnID) + ", not " + endpoint.output.Type + " encoded by " + endpoint.output.Name)
		}
	} else if !canonicalVoidType(c, f.returnID) {
		return refuse("returns " + c.displayTypeID(f.returnID) + "; an endpoint without output takes a void handler")
	}
	return true
}

// endpointReachableFailures lists, in first-reached order, every named
// failure the endpoint can raise apart from its profile-owned boundary
// failures, with the origins that raise it.
func (c *checker) endpointReachableFailures(endpoint *EndpointDeclaration) ([]string, map[string][]EndpointFailureOrigin) {
	order := []string{}
	origins := map[string][]EndpointFailureOrigin{}
	add := func(f *Function, origin EndpointFailureOrigin) {
		for _, name := range f.Errors {
			if _, known := c.program.Errors[name]; !known {
				continue
			}
			if origins[name] == nil {
				order = append(order, name)
			}
			origins[name] = append(origins[name], origin)
		}
	}
	if endpoint.input != nil {
		for _, witness := range endpoint.input.transformations() {
			add(witness.transform.decode, EndpointFailureOrigin{Stage: "input", Function: witness.transform.decode.Identity, Witness: witness.Name})
		}
	}
	if endpoint.handler != nil {
		add(endpoint.handler, EndpointFailureOrigin{Stage: "handler", Function: endpoint.handler.Identity})
	}
	if endpoint.output != nil {
		for _, witness := range endpoint.output.transformations() {
			add(witness.transform.encode, EndpointFailureOrigin{Stage: "output", Function: witness.transform.encode.Identity, Witness: witness.Name})
		}
	}
	return order, origins
}

// checkEndpointFailures checks the failure mapping: every reachable named
// failure is mapped exactly once to a 4xx or 5xx status, optionally with a
// failure witness of that failure; no entry maps an unreachable failure.
// Malformed input (400) and failed response encoding (500) are the
// profile's boundary failures and are not mappable.
func (c *checker) checkEndpointFailures(endpoint *EndpointDeclaration, witnesses map[string]*CodecDeclaration) bool {
	ok := true
	refuse := func(message string, span Span) {
		c.diagnostic(endpointDerivationCode, "endpoint "+endpoint.Name+" "+message, span)
		ok = false
	}
	reachable, origins := c.endpointReachableFailures(endpoint)
	mapped := map[string]bool{}
	for _, entry := range endpoint.Failures {
		switch {
		case mapped[entry.Name]:
			refuse("maps failure "+entry.Name+" twice", entry.Span)
			continue
		case c.errors[entry.Name] == nil:
			c.diagnostic("EF102", "unknown failure "+entry.Name+" in endpoint "+endpoint.Name, entry.Span)
			ok = false
			continue
		case entry.Name == jsonCodecDerivation.DecodeFailure || entry.Name == jsonCodecDerivation.EncodeFailure:
			refuse("cannot map "+entry.Name+": malformed input answers 400 and a failed response encoding answers 500; recover a handler's own "+entry.Name+" before it reaches the endpoint", entry.Span)
			continue
		}
		mapped[entry.Name] = true
		mapping := &endpointMapping{failure: entry.Name, status: entry.Status, origins: origins[entry.Name]}
		if entry.Status < 400 || entry.Status > 599 {
			refuse("maps "+entry.Name+" to status "+strconv.FormatInt(entry.Status, 10)+"; a failure status is 4xx or 5xx", entry.StatusSpan)
		}
		if origins[entry.Name] == nil {
			refuse("maps "+entry.Name+", which its input, handler and output never raise", entry.Span)
		}
		if entry.Codec != "" {
			codec := witnesses[entry.Codec]
			switch {
			case codec == nil:
				c.diagnostic("EF102", "unknown codec witness "+entry.Codec+" for endpoint "+endpoint.Name+" failure "+entry.Name, entry.CodecSpan)
				ok = false
			case !codec.failure || codec.Type != entry.Name:
				refuse("failure "+entry.Name+" body "+codec.Name+" must be a failure witness of "+entry.Name+", such as derive "+codec.Name+" = Json.codec<"+entry.Name+">(...)", entry.CodecSpan)
			case codec.plan == nil:
				ok = false
			default:
				mapping.codec = codec
			}
		}
		endpoint.mappings = append(endpoint.mappings, mapping)
	}
	for _, name := range reachable {
		if !mapped[name] {
			origin := origins[name][0]
			source := "handler " + endpoint.handler.Name
			if origin.Stage != "handler" {
				source = origin.Stage + " witness " + origin.Witness
			}
			remedy := "add " + name + ": <status> to its raises clause or recover it"
			if name == jsonCodecDerivation.DecodeFailure || name == jsonCodecDerivation.EncodeFailure {
				remedy = "the profile answers only its own boundary " + name + "; recover it before it reaches the endpoint"
			}
			c.diagnostic("EF107", "endpoint "+endpoint.Name+" does not map failure "+name+" raised by its "+source+"; "+remedy, endpoint.Span)
			ok = false
		}
	}
	return ok
}

// endpointStaticPath reports whether path is an exact static path the
// profile matches byte for byte.
func endpointStaticPath(path string) bool {
	if !strings.HasPrefix(path, "/") {
		return false
	}
	for _, r := range path {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', strings.ContainsRune("-._~/", r):
		default:
			return false
		}
	}
	return true
}

// endpointFunction registers the synthesized handler function of an
// endpoint or route table, taking the request and returning the reply.
func endpointFunction(c *checker, name string, errors, services []string, body *Expr, binding *localBinding, endpoint *EndpointDeclaration) *Function {
	f := &Function{
		Name:     name,
		Module:   currentModuleIdentity,
		SourceID: "source:user",
		Owner:    "module",
		Params:   []Param{{Name: "request", Type: "HttpRequest", binding: binding}},
		Return:   "HttpReply",
		Effect:   true,
		Errors:   errors,
		Services: services,
		Body:     &Block{Statements: []*Statement{{Kind: "expr", Value: body}}},
		Span:     endpoint.Span,
		DeclSpan: endpoint.Span,
		endpoint: endpoint,
	}
	f.Identity = c.declarationIdentity("function", f.Owner, name)
	c.signature(f)
	c.functions[name] = f
	c.program.DerivedFunctions = append(c.program.DerivedFunctions, f)
	return f
}

// rows are the declared rows of an endpoint's handler function: the
// terminal JsonEncodeFailure when it encodes any body, and every service its
// input decoding, handler and output encoding need. Mapped failures are
// answered, and malformed input is answered 400, so neither remains.
func (endpoint *EndpointDeclaration) rows() ([]string, []string) {
	errors, services := []string{}, union(nil, endpoint.handler.Services)
	encodes := endpoint.output != nil
	for _, mapping := range endpoint.mappings {
		encodes = encodes || mapping.codec != nil
	}
	if encodes {
		errors = append(errors, jsonCodecDerivation.EncodeFailure)
	}
	if endpoint.input != nil {
		services = union(services, endpoint.input.decode.Services)
	}
	if endpoint.output != nil {
		services = union(services, endpoint.output.encode.Services)
	}
	return errors, services
}

// synthesizeEndpoint derives the endpoint's handler function. Its body is
// one endpoint operation whose arguments reference, as ordinary function
// values, the handler, the input witness's decode and the output witness's
// encode, in that order: the operation runs exactly these, so the
// application plan retains exactly them and their transformations.
func (c *checker) synthesizeEndpoint(endpoint *EndpointDeclaration) {
	binding := &localBinding{Kind: "parameter", Name: "request"}
	args := []*Expr{{Kind: "name", Name: endpoint.handler.Name}}
	direction := func(codec *CodecDeclaration, name string) *Expr {
		return &Expr{Kind: "member", Left: &Expr{Kind: "name", Name: codec.Name}, Name: name}
	}
	if endpoint.input != nil {
		args = append(args, direction(endpoint.input, "decode"))
	}
	if endpoint.output != nil {
		args = append(args, direction(endpoint.output, "encode"))
	}
	operation := &Expr{Kind: "endpoint", Name: endpoint.Name, Left: &Expr{Kind: "name", Name: "request", binding: binding}, Args: args, endpoint: endpoint}
	errors, services := endpoint.rows()
	endpoint.function = endpointFunction(c, endpoint.Name, errors, services, operation, binding, endpoint)
}

// checkRoutes resolves a route table: each entry is a checked endpoint, and
// no two entries serve the same method and path.
func (c *checker) checkRoutes(table *EndpointDeclaration, named map[string]*EndpointDeclaration) bool {
	ok := true
	if len(table.Routes) == 0 {
		c.diagnostic(endpointDerivationCode, "route table "+table.Name+" lists no endpoints", table.Span)
		return false
	}
	served := map[string]string{}
	listed := map[string]bool{}
	for _, route := range table.Routes {
		endpoint := named[route.Name]
		switch {
		case endpoint == nil:
			c.diagnostic("EF102", "unknown endpoint "+route.Name+" in route table "+table.Name, route.Span)
			ok = false
		case listed[route.Name]:
			c.diagnostic(endpointDerivationCode, "route table "+table.Name+" lists "+route.Name+" twice", route.Span)
			ok = false
		case endpoint.Member != "endpoint":
			c.diagnostic(endpointDerivationCode, "route table "+table.Name+" entry "+route.Name+" is not an endpoint", route.Span)
			ok = false
		case endpoint.function == nil:
			ok = false
		default:
			key := endpoint.method + " " + endpoint.path
			if previous := served[key]; previous != "" {
				c.diagnostic(endpointDerivationCode, "route table "+table.Name+" serves "+key+" with both "+previous+" and "+route.Name, route.Span)
				ok = false
			}
			served[key] = route.Name
			table.endpoints = append(table.endpoints, endpoint)
		}
		listed[route.Name] = true
	}
	return ok
}

func (c *checker) synthesizeRoutes(table *EndpointDeclaration) {
	binding := &localBinding{Kind: "parameter", Name: "request"}
	errors, services := []string{}, []string{}
	args := []*Expr{}
	for _, endpoint := range table.endpoints {
		errors = union(errors, endpoint.function.Errors)
		services = union(services, endpoint.function.Services)
		args = append(args, &Expr{Kind: "name", Name: endpoint.Name})
	}
	operation := &Expr{Kind: "routes", Name: table.Name, Left: &Expr{Kind: "name", Name: "request", binding: binding}, Args: args, endpoint: table}
	table.function = endpointFunction(c, table.Name, errors, services, operation, binding, table)
}

// endpointOperation checks the body of a synthesized endpoint or route
// table. Its rows are its function's declared rows: the mapped failures are
// answered, so only the terminal encoding failure and the services remain.
func (c *checker) endpointOperation(e *Expr, env localEnv) checkedExpression {
	c.expr(e.Left, env, false)
	for _, arg := range e.Args {
		c.expr(arg, env, false)
	}
	f := e.endpoint.function
	if f == nil {
		return c.checkedData("invalid")
	}
	if len(f.Errors) > 0 {
		c.reasons = append(c.reasons, Contribution{"failure", f.Errors, e.Span})
	}
	if len(f.Services) > 0 {
		c.reasons = append(c.reasons, Contribution{"requirement", f.Services, e.Span})
	}
	t := c.checkedDataID(c.canonicalRef(c.typeRef("HttpReply")), nil, nil)
	t.evaluation = c.evaluation(c.internRow(f.Errors), c.internRow(f.Services))
	return t
}

// endpointOperation retains what an endpoint or route table body runs
// beyond its referenced functions: the endpoint lowering helpers and, for
// each mapped failure with a failure witness, that witness's plan, the
// codec runtime module and lowering helpers, and the failure declaration
// whose payload it encodes.
func (p *applicationPlanner) endpointOperation(e *Expr, owner applicationOrigin) {
	if e.endpoint == nil || e.endpoint.function == nil {
		p.err = fmt.Errorf("application endpoint operation at offset %d has no checked endpoint", e.Span.Offset)
		return
	}
	p.helper(endpointLoweringHelper, owner)
	for _, mapping := range e.endpoint.mappings {
		if mapping.codec == nil {
			continue
		}
		plan := mapping.codec.plan
		planOrigin := applicationOrigin{RequiresCodecPlan, plan.ID}
		if p.require(RequiresCodecPlan, plan.ID, owner, "endpoint-failure") {
			for _, node := range plan.Nodes {
				p.typeID(node.typeID, planOrigin)
			}
		}
		p.runtimeModule(rt.RuntimeModuleCodec, planOrigin, "codec-plan")
		p.helper(codecLoweringHelper, owner)
		p.failure(mapping.failure, owner)
	}
}

// EndpointInspection is the public, target-independent view of one endpoint
// or route table. Function is the synthesized handler and Failures and
// Requirements are its checked primary rows. Responses lists every mapped
// failure with the origins that raise it; Terminal is the separate
// fail-closed policy of response encoding: a failed success or failure-body
// encoding is never mapped and the transport answers its generic 500.
type EndpointInspection struct {
	Name         string                 `json:"name"`
	Kind         string                 `json:"kind"`
	Function     string                 `json:"function"`
	Method       string                 `json:"method,omitempty"`
	Path         string                 `json:"path,omitempty"`
	Status       int64                  `json:"status,omitempty"`
	Input        string                 `json:"input,omitempty"`
	Handler      string                 `json:"handler,omitempty"`
	Output       string                 `json:"output,omitempty"`
	Responses    []EndpointResponse     `json:"responses,omitempty"`
	Terminal     *EndpointTerminal      `json:"terminal,omitempty"`
	Routes       []string               `json:"routes,omitempty"`
	Failures     []string               `json:"failures"`
	Requirements []string               `json:"requirements"`
	Span         Span                   `json:"span"`
	Malformed    *EndpointMalformedRule `json:"malformed,omitempty"`
}

// EndpointResponse is one mapped failure: its status, the failure witness
// encoding its body, if any, and where the endpoint can raise it.
type EndpointResponse struct {
	Failure string                  `json:"failure"`
	Status  int64                   `json:"status"`
	Body    string                  `json:"body,omitempty"`
	Origins []EndpointFailureOrigin `json:"origins"`
}

// EndpointTerminal is the terminal response-encoding policy.
type EndpointTerminal struct {
	Failure string `json:"failure"`
	Status  int64  `json:"status"`
}

// EndpointMalformedRule is the input profile: the media type the endpoint
// requires and the status of its input witness's structural failure.
type EndpointMalformedRule struct {
	MediaType       string `json:"mediaType"`
	UnsupportedType int64  `json:"unsupportedMediaType"`
	Failure         string `json:"failure"`
	Status          int64  `json:"status"`
}

// publishEndpoints projects every synthesized endpoint and route table once
// its function has been checked, reading the function's checked rows.
func (c *checker) publishEndpoints() {
	for _, endpoint := range c.program.Endpoints {
		f := endpoint.function
		if f == nil {
			continue
		}
		inspection := EndpointInspection{
			Name:         endpoint.Name,
			Kind:         endpoint.Member,
			Function:     f.Identity,
			Failures:     append([]string{}, c.rowLabels(f.failureID)...),
			Requirements: append([]string{}, c.rowLabels(f.serviceID)...),
			Span:         endpoint.Span,
		}
		if endpoint.Member == "routes" {
			for _, route := range endpoint.endpoints {
				inspection.Routes = append(inspection.Routes, route.Name)
			}
			c.result.Endpoints = append(c.result.Endpoints, inspection)
			continue
		}
		inspection.Method, inspection.Path, inspection.Status, inspection.Handler = endpoint.method, endpoint.path, endpoint.status, endpoint.handler.Identity
		if endpoint.input != nil {
			inspection.Input = endpoint.input.Name
			inspection.Malformed = &EndpointMalformedRule{MediaType: "application/json", UnsupportedType: 415, Failure: jsonCodecDerivation.DecodeFailure, Status: 400}
		}
		if endpoint.output != nil {
			inspection.Output = endpoint.output.Name
		}
		for _, mapping := range endpoint.mappings {
			response := EndpointResponse{Failure: mapping.failure, Status: mapping.status, Origins: append([]EndpointFailureOrigin{}, mapping.origins...)}
			if mapping.codec != nil {
				response.Body = mapping.codec.Name
			}
			inspection.Responses = append(inspection.Responses, response)
		}
		if slices.Contains(f.Errors, jsonCodecDerivation.EncodeFailure) {
			inspection.Terminal = &EndpointTerminal{Failure: jsonCodecDerivation.EncodeFailure, Status: 500}
		}
		c.result.Endpoints = append(c.result.Endpoints, inspection)
	}
}
