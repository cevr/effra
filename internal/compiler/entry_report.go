package compiler

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// The entry report plan is the checked rendering schema for the entry
// failure report (docs/design.md, "Entry failure report"). It is built once
// from main's failure row and the checked types of each failure's fields, and
// both targets render a failed entry from it, keyed by type rather than by
// the runtime shape of a value. Like a codec plan, it is target-neutral; each
// target lowers it to a table its runtime interprets.
type entryReportPlan struct {
	failures []entryReportFailure
	nodes    []entryReportNode
}

// entryReportFailure is one label of main's failure row. A built-in failure
// declares no fields but may carry diagnostic text, reported as `message`.
type entryReportFailure struct {
	tag        string
	diagnostic bool
	fields     []entryReportField
}

type entryReportNode struct {
	kind     string
	goType   string
	fields   []entryReportField
	variants []entryReportVariant
}

type entryReportVariant struct {
	name   string
	jsTag  string
	goType string
	fields []entryReportField
}

type entryReportField struct {
	name string
	node int
}

// Built-in failures that carry no diagnostic text. Every other built-in or
// bundled failure carries its message (assertions, I/O, Go and codec errors).
var entryReportSilentFailures = map[string]bool{"Timeout": true}

// entryReportPlan describes main's failure row. A declared failure that the
// target does not retain cannot occur there and is left out with its types;
// a nil retained keeps every label.
func (r *Result) entryReportPlan(retained func(string) bool) *entryReportPlan {
	plan := &entryReportPlan{}
	main := r.Find("main")
	if main == nil || r.Program == nil || r.Program.semantic == nil {
		return plan
	}
	c := r.Program.semantic
	b := entryReportBuilder{c: c, plan: plan, indices: map[TypeID]int{}}
	tags := slices.Clone(main.Contract.Errors)
	slices.Sort(tags)
	for _, tag := range slices.Compact(tags) {
		decl := c.errors[tag]
		declared := decl != nil && slices.Contains(c.program.ErrorDecls, decl)
		if declared && retained != nil && !retained(tag) {
			continue
		}
		failure := entryReportFailure{tag: tag}
		switch {
		case declared:
			failure.fields = b.fields(decl.Fields)
		case decl != nil:
			failure.diagnostic = !entryReportSilentFailures[tag]
		}
		plan.failures = append(plan.failures, failure)
	}
	return plan
}

type entryReportBuilder struct {
	c       *checker
	plan    *entryReportPlan
	indices map[TypeID]int
}

// fields lists declared fields in byte order of their source names, the
// report's portable field order.
func (b *entryReportBuilder) fields(declared []Field) []entryReportField {
	out := make([]entryReportField, 0, len(declared))
	for _, field := range declared {
		out = append(out, entryReportField{name: field.Name, node: b.node(field.typeID)})
	}
	slices.SortFunc(out, func(a, b entryReportField) int { return strings.Compare(a.name, b.name) })
	return out
}

func (b *entryReportBuilder) node(id TypeID) int {
	if index, ok := b.indices[id]; ok {
		return index
	}
	index := len(b.plan.nodes)
	b.indices[id] = index
	// Reserve the index first so recursive data refers back to it.
	b.plan.nodes = append(b.plan.nodes, entryReportNode{kind: "opaque"})
	node := b.describe(id)
	b.plan.nodes[index] = node
	return index
}

func (b *entryReportBuilder) describe(id TypeID) entryReportNode {
	c := b.c
	n := c.node(id)
	if n == nil {
		return entryReportNode{kind: "opaque"}
	}
	goType := canonicalGoType(c, id, map[TypeID]bool{})
	switch n.Kind {
	case "primitive":
		switch n.Name {
		case "string", "i64", "bool", "bytes":
			return entryReportNode{kind: n.Name, goType: goType}
		case voidTypeName:
			return entryReportNode{kind: "void"}
		}
	case "callable", "callable-shape":
		return entryReportNode{kind: "fn"}
	case "record":
		if r := c.records[n.Name]; r != nil && len(r.Parameters) == 0 {
			return entryReportNode{kind: "record", goType: goType, fields: b.fields(r.Fields)}
		}
	case "error":
		if decl := c.errors[n.Name]; decl != nil {
			return entryReportNode{kind: "record", goType: goType, fields: b.fields(decl.Fields)}
		}
	case "enum":
		if e := c.enums[n.Name]; e != nil && len(e.Parameters) == 0 {
			variants := []entryReportVariant{}
			for _, variant := range e.Variants {
				variants = append(variants, entryReportVariant{name: variant.Name, jsTag: e.Name + "." + variant.Name, goType: goVariantType(e.Name, variant.Name), fields: b.fields(variant.Fields)})
			}
			return entryReportNode{kind: "enum", variants: variants}
		}
	case "application":
		template := c.templates[n.Declaration]
		if template == nil || len(template.Parameters) != len(n.Args) {
			break
		}
		if template.Kind == "record" {
			fields, ok := c.instantiateDataFields(template, template.Fields, n.Args)
			if ok {
				return entryReportNode{kind: "record", goType: goType, fields: b.fields(fields)}
			}
			break
		}
		variants, ok := c.applicationVariants(id)
		if !ok {
			break
		}
		args := []string{}
		for _, arg := range n.Args {
			args = append(args, canonicalGoType(c, arg, map[TypeID]bool{}))
		}
		out := []entryReportVariant{}
		for _, variant := range variants {
			out = append(out, entryReportVariant{name: variant.Name, jsTag: template.Identity + "." + variant.Name, goType: goVariantType("template_"+template.EmissionName, variant.Name) + "[" + strings.Join(args, ",") + "]", fields: b.fields(variant.Fields)})
		}
		return entryReportNode{kind: "enum", variants: out}
	}
	return entryReportNode{kind: "opaque"}
}

// goEntryReport lowers the plan to the native runtime's table. Field reads
// are typed accessors on the generated types, so the runtime never inspects
// a value's shape. Failures whose declaration the application plan does not
// retain are left out, since their Go types are not emitted.
func (r *Result) goEntryReport(retained func(string) bool) string {
	plan := r.entryReportPlan(retained)
	var out strings.Builder
	fields := func(owner string, fields []entryReportField) string {
		parts := []string{}
		for _, field := range fields {
			parts = append(parts, "{Name:"+strconv.Quote(field.name)+",Node:"+strconv.Itoa(field.node)+",Get:func(v any)any{return v.("+owner+")."+goFieldName(field.name)+"}}")
		}
		return "[]er.ReportField{" + strings.Join(parts, ",") + "}"
	}
	kinds := map[string]string{"opaque": "er.ReportOpaque", "string": "er.ReportString", "i64": "er.ReportI64", "bool": "er.ReportBool", "void": "er.ReportVoid", "bytes": "er.ReportBytes", "fn": "er.ReportFn", "record": "er.ReportRecord", "enum": "er.ReportEnum"}
	out.WriteString("var efEntryReport=er.EntryReportPlan{Failures:map[string]er.ReportFailure{")
	for _, failure := range plan.failures {
		out.WriteString(strconv.Quote(failure.tag) + ":{Diagnostic:" + strconv.FormatBool(failure.diagnostic))
		if len(failure.fields) > 0 {
			out.WriteString(",Fields:" + fields("efType_"+goIdent(failure.tag), failure.fields))
		}
		out.WriteString("},")
	}
	out.WriteString("},Nodes:[]er.ReportNode{")
	for _, node := range plan.nodes {
		out.WriteString("{Kind:" + kinds[node.kind])
		if node.kind == "record" {
			out.WriteString(",Fields:" + fields(node.goType, node.fields))
		}
		if node.kind == "enum" {
			out.WriteString(",Variants:[]er.ReportVariant{")
			for _, variant := range node.variants {
				out.WriteString("{Name:" + strconv.Quote(variant.name) + ",Match:func(v any)(any,bool){x,ok:=v.(" + variant.goType + ");return x,ok},Fields:" + fields(variant.goType, variant.fields) + "},")
			}
			out.WriteString("}")
		}
		out.WriteString("},")
	}
	out.WriteString("}}\n")
	return out.String()
}

type jsEntryReportField struct {
	Name string `json:"name"`
	Node int    `json:"node"`
}

type jsEntryReportVariant struct {
	Tag    string               `json:"tag"`
	Name   string               `json:"name"`
	Fields []jsEntryReportField `json:"fields"`
}

type jsEntryReportNode struct {
	Kind     string                 `json:"kind"`
	Fields   []jsEntryReportField   `json:"fields,omitempty"`
	Variants []jsEntryReportVariant `json:"variants,omitempty"`
}

type jsEntryReportFailure struct {
	Tag        string               `json:"tag"`
	Diagnostic bool                 `json:"diagnostic"`
	Fields     []jsEntryReportField `json:"fields"`
}

// jsEntryReport lowers the plan to the JavaScript entry's table. A variant
// is found by its discriminator only where the checked type is an enum, so a
// record field named `_tag` stays ordinary data.
func (r *Result) jsEntryReport() string {
	plan := r.entryReportPlan(nil)
	fields := func(fields []entryReportField) []jsEntryReportField {
		out := []jsEntryReportField{}
		for _, field := range fields {
			out = append(out, jsEntryReportField{field.name, field.node})
		}
		return out
	}
	table := struct {
		Failures []jsEntryReportFailure `json:"failures"`
		Nodes    []jsEntryReportNode    `json:"nodes"`
	}{Failures: []jsEntryReportFailure{}, Nodes: []jsEntryReportNode{}}
	for _, failure := range plan.failures {
		table.Failures = append(table.Failures, jsEntryReportFailure{failure.tag, failure.diagnostic, fields(failure.fields)})
	}
	for _, node := range plan.nodes {
		lowered := jsEntryReportNode{Kind: node.kind, Fields: fields(node.fields)}
		if node.kind != "record" {
			lowered.Fields = nil
		}
		for _, variant := range node.variants {
			lowered.Variants = append(lowered.Variants, jsEntryReportVariant{variant.jsTag, variant.name, fields(variant.fields)})
		}
		table.Nodes = append(table.Nodes, lowered)
	}
	data, err := json.Marshal(table)
	if err != nil {
		panic(err)
	}
	return "const __ef_entryReport = " + string(data) + ";\n"
}
