package compiler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

const interfaceSummarySchema = 2
const ownershipSummarySchema = 2
const maxInterfaceSummaryBytes = 1 << 20
const maxInterfaceClosureBytes = 8 << 20
const maxInterfaceTableEntries = 4096

// These DTOs are compiler-private executable interface transport. They are
// deliberately independent from the bounded explanatory projection schema.
// Every field is required, including false, empty and absent alternatives.
type interfaceSummary struct {
	Templates       []summaryTemplate    `json:"templates"`
	InterfaceSchema int                  `json:"interfaceSchema"`
	OwnershipSchema int                  `json:"ownershipSchema"`
	SemanticABI     string               `json:"semanticABI"`
	Module          string               `json:"module"`
	Version         string               `json:"version"`
	ContentHash     string               `json:"contentHash"`
	SourceInput     string               `json:"sourceInput"`
	Producer        string               `json:"producer"`
	Implementation  string               `json:"implementation"`
	Sources         []SourceInfo         `json:"sources"`
	Declarations    []summaryDeclaration `json:"declarations"`
	Types           []summaryType        `json:"types"`
	Rows            []summaryRow         `json:"rows"`
	Evidence        []summaryEvidence    `json:"evidence"`
	Occurrences     []summaryOccurrence  `json:"occurrences"`
	Relations       []summaryRelation    `json:"relations"`
	TrustedHost     []Binding            `json:"trustedHost"`
}
type summaryTemplate struct {
	Ref        string                     `json:"ref"`
	Source     string                     `json:"source"`
	Parameters []summaryTemplateParameter `json:"parameters"`
	Fields     []summaryTemplateField     `json:"fields"`
}
type summaryTemplateParameter struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Ref      string `json:"ref"`
	Variable string `json:"variable"`
	Shape    string `json:"shape"`
}
type summaryTemplateField struct {
	Name      string `json:"name"`
	Parameter int    `json:"parameter"`
}

type summaryDeclaration struct {
	Ref            string             `json:"ref"`
	Source         string             `json:"source"`
	Signature      string             `json:"signature"`
	Parameters     []summaryParameter `json:"parameters"`
	Ownership      []summaryOwner     `json:"ownership"`
	Captures       []summaryOwner     `json:"captures"`
	ReturnEvidence string             `json:"returnEvidence"`
	Body           string             `json:"body"`
}
type summaryParameter struct {
	Name string `json:"name"`
	Type string `json:"type"`
}
type summaryType struct {
	Ref         string   `json:"ref"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Declaration string   `json:"declaration"`
	Mode        string   `json:"mode"`
	Arguments   []string `json:"arguments"`
	Result      string   `json:"result"`
	Failures    string   `json:"failures"`
	Services    string   `json:"services"`
}
type summaryRow struct {
	Ref        string         `json:"ref"`
	Labels     []string       `json:"labels"`
	Parameters []RowParameter `json:"parameters"`
}
type summaryParameterBinding struct {
	Kind        string `json:"kind"`
	Declaration string `json:"declaration"`
	Ordinal     int    `json:"ordinal"`
	Path        string `json:"path"`
}
type summaryReference struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}
type summaryEvidence struct {
	Ref        string                  `json:"ref"`
	Callees    []string                `json:"callees"`
	Parameter  summaryParameterBinding `json:"parameter"`
	Unresolved bool                    `json:"unresolved"`
}
type summaryRows struct {
	Failures string `json:"failures"`
	Services string `json:"services"`
}
type summaryOccurrence struct {
	Fields     []summaryFieldOccurrence `json:"fields"`
	Ref        string                   `json:"ref"`
	Contract   string                   `json:"contract"`
	Ownership  []summaryOwner           `json:"ownership"`
	Captures   []summaryOwner           `json:"captures"`
	Child      []summaryOwner           `json:"child"`
	Evidence   string                   `json:"evidence"`
	Evaluation summaryRows              `json:"evaluation"`
	Executed   summaryRows              `json:"executed"`
}
type summaryFieldOccurrence struct {
	Name       string `json:"name"`
	Occurrence string `json:"occurrence"`
}
type summaryRelation struct {
	Ref       string   `json:"ref"`
	Evidence  string   `json:"evidence"`
	Arguments []string `json:"arguments"`
	Result    string   `json:"result"`
	Path      string   `json:"path"`
}
type summaryRegion struct {
	Kind        string `json:"kind"`
	Declaration string `json:"declaration"`
	Ordinal     int    `json:"ordinal"`
}
type summaryOwner struct {
	Path                string           `json:"path"`
	Status              string           `json:"status"`
	Region              summaryRegion    `json:"region"`
	Origin              string           `json:"origin"`
	SourcePath          string           `json:"sourcePath"`
	SourceSet           bool             `json:"sourceSet"`
	OwnerKind           string           `json:"ownerKind"`
	PotentialOwner      bool             `json:"potentialOwner"`
	Remainder           bool             `json:"remainder"`
	RemainderExclusions string           `json:"remainderExclusions"`
	Relation            summaryReference `json:"relation"`
}

var summaryOwnerKinds = [...]string{"unknown", "parameter", "deferred", "invocation-result", "lexical", "child", "timeout", "callback-result"}

type summaryExporter struct {
	c            *checker
	dto          interfaceSummary
	types        map[TypeID]bool
	rows         map[RowID]bool
	evidence     map[callableEvidence]string
	relations    map[*callbackResultRelation]string
	visiting     map[*callbackResultRelation]bool
	declarations map[string]*Function
	err          error
}

func exportInterfaceSummary(c *checker, module, sourceInput string, functions []*Function) (interfaceSummary, error) {
	x := summaryExporter{c: c, types: map[TypeID]bool{}, rows: map[RowID]bool{}, evidence: map[callableEvidence]string{}, relations: map[*callbackResultRelation]string{}, visiting: map[*callbackResultRelation]bool{}, declarations: map[string]*Function{}}
	x.dto = interfaceSummary{Templates: []summaryTemplate{}, InterfaceSchema: interfaceSummarySchema, OwnershipSchema: ownershipSummarySchema, SemanticABI: SemanticProducerIdentity, Module: module, Version: bundledInterfaceVersion, SourceInput: sourceInput, Producer: SemanticProducerIdentity, Implementation: sourceInput, Sources: []SourceInfo{}, Declarations: []summaryDeclaration{}, Types: []summaryType{}, Rows: []summaryRow{}, Evidence: []summaryEvidence{}, Occurrences: []summaryOccurrence{}, Relations: []summaryRelation{}, TrustedHost: []Binding{}}
	for _, r := range c.program.BundledTemplates {
		if r.Module != module {
			continue
		}
		d := summaryTemplate{Ref: r.Identity, Source: r.SourceID, Parameters: []summaryTemplateParameter{}, Fields: []summaryTemplateField{}}
		for _, p := range r.Parameters {
			shape := ""
			if p.shapeID != invalidTypeID {
				shape = x.typ(p.shapeID)
			}
			d.Parameters = append(d.Parameters, summaryTemplateParameter{Name: p.Name, Kind: p.Kind, Ref: p.Identity, Variable: x.typ(p.typeID), Shape: shape})
		}
		for _, field := range r.Fields {
			ordinal := slices.IndexFunc(r.Parameters, func(p TemplateParameter) bool { return p.Name == field.Type })
			d.Fields = append(d.Fields, summaryTemplateField{Name: field.Name, Parameter: ordinal})
		}
		x.dto.Templates = append(x.dto.Templates, d)
	}
	slices.SortFunc(x.dto.Templates, func(a, b summaryTemplate) int { return strings.Compare(a.Ref, b.Ref) })
	for _, f := range functions {
		x.declarations[f.Identity] = f
	}
	for _, source := range c.result.Sources {
		if source.Module == module {
			x.dto.Sources = append(x.dto.Sources, source)
		}
	}
	ordered := append([]*Function{}, functions...)
	slices.SortFunc(ordered, func(a, b *Function) int { return strings.Compare(a.Identity, b.Identity) })
	var implementation strings.Builder
	implementation.WriteString(c.result.Target)
	for _, d := range x.dto.Templates {
		r := c.templates[d.Ref]
		implementation.WriteString("\x00" + r.Identity + "\x00")
		if c.result.Target == "go" {
			implementation.WriteString(goTemplateDeclaration(r))
		} else {
			implementation.WriteString(jsTemplateDeclaration(r))
		}
	}
	g := &goEmitter{program: c.program}
	for _, f := range ordered {
		checked, ok := c.result.checkedFunctions[f]
		if !ok {
			return interfaceSummary{}, fmt.Errorf("summary requires finalized declaration %s", f.Identity)
		}
		d := summaryDeclaration{Ref: f.Identity, Source: f.SourceID, Signature: x.typ(checked.contract.valueID()), Parameters: []summaryParameter{}, Ownership: x.facts(f, f.Ownership, 0), Captures: x.facts(f, f.Captures, 0), ReturnEvidence: x.callable(f.returnCallableEvidence), Body: x.occurrence(f, checked.body, 0)}
		for _, p := range f.Params {
			d.Parameters = append(d.Parameters, summaryParameter{Name: p.Name, Type: x.typ(p.typeID)})
		}
		x.dto.Declarations = append(x.dto.Declarations, d)
		implementation.WriteString("\x00" + f.Identity + "\x00")
		if c.result.Target == "go" {
			implementation.WriteString(g.functionDeclaration(f))
		} else {
			implementation.WriteString(jsFunction(f))
		}
	}
	x.dto.Implementation = formatDigest(implementation.String())
	if x.err != nil {
		return interfaceSummary{}, x.err
	}
	slices.SortFunc(x.dto.Types, func(a, b summaryType) int { return strings.Compare(a.Ref, b.Ref) })
	slices.SortFunc(x.dto.Rows, func(a, b summaryRow) int { return strings.Compare(a.Ref, b.Ref) })
	data, err := json.Marshal(x.dto)
	if err != nil || len(data) > maxInterfaceSummaryBytes {
		return interfaceSummary{}, fmt.Errorf("interface summary exceeds byte budget")
	}
	x.dto.ContentHash = formatDigest(string(data))
	return x.dto, nil
}

func (x *summaryExporter) typ(id TypeID) string {
	n := x.c.node(id)
	if n == nil {
		x.err = fmt.Errorf("summary contains unknown type")
		return ""
	}
	ref := x.c.typeNodeID(id)
	if x.types[id] {
		return ref
	}
	x.types[id] = true
	if len(x.types) > maxInterfaceTableEntries {
		x.err = fmt.Errorf("summary type budget exceeded")
		return ""
	}
	d := summaryType{Ref: ref, Kind: n.Kind, Name: n.Name, Declaration: n.Declaration, Mode: n.Mode, Arguments: []string{}, Failures: x.row(n.FailureRow), Services: x.row(n.ServiceRow)}
	for _, arg := range n.Args {
		d.Arguments = append(d.Arguments, x.typ(arg))
	}
	if n.Result != invalidTypeID {
		d.Result = x.typ(n.Result)
	}
	x.dto.Types = append(x.dto.Types, d)
	return ref
}

func (x *summaryExporter) row(id RowID) string {
	if id == emptyRowID {
		return ""
	}
	if x.rows[id] {
		return x.c.rowNodeID(id)
	}
	x.rows[id] = true
	row := x.c.rows[id-1]
	x.dto.Rows = append(x.dto.Rows, summaryRow{Ref: x.c.rowNodeID(id), Labels: append([]string{}, row.Labels...), Parameters: append([]RowParameter{}, row.Parameters...)})
	return x.c.rowNodeID(id)
}

func (x *summaryExporter) callable(e callableEvidence) string {
	if ref, ok := x.evidence[e]; ok {
		return ref
	}
	ref := "evidence:" + strconv.Itoa(len(x.dto.Evidence))
	x.evidence[e] = ref
	d := summaryEvidence{Ref: ref, Callees: []string{}, Parameter: summaryParameterBinding{Kind: "absent", Ordinal: -1}, Unresolved: e.unresolved}
	if e.count < 0 || e.count > len(e.callees) {
		x.err = fmt.Errorf("invalid known callee count")
		return ""
	}
	for _, f := range e.callees[:e.count] {
		if f == nil || x.declarations[f.Identity] != f {
			x.err = fmt.Errorf("summary callee is outside declaration closure")
			return ""
		}
		d.Callees = append(d.Callees, f.Identity)
	}
	slices.Sort(d.Callees)
	if e.parameter != nil {
		ordinal := -1
		for i, p := range e.parameter.Params {
			if p.Name == e.parameterName {
				ordinal = i
			}
		}
		if ordinal < 0 || x.declarations[e.parameter.Identity] != e.parameter {
			x.err = fmt.Errorf("invalid callable parameter binding")
			return ""
		}
		d.Parameter = summaryParameterBinding{Kind: "parameter", Declaration: e.parameter.Identity, Ordinal: ordinal, Path: e.parameterPath}
	}
	x.dto.Evidence = append(x.dto.Evidence, d)
	return ref
}

func (x *summaryExporter) region(f *Function, region string) summaryRegion {
	d := summaryRegion{Ordinal: -1}
	switch region {
	case "":
		d.Kind = "unknown"
	case "*":
		d.Kind = "wildcard"
	case "invocation":
		d.Kind = "invocation"
	case "deferred":
		d.Kind = "deferred"
	case "parameter:*":
		d.Kind = "parameter-wildcard"
	default:
		if name, ok := strings.CutPrefix(region, "parameter:"); ok {
			for i, p := range f.Params {
				if p.Name == name {
					return summaryRegion{Kind: "parameter", Declaration: f.Identity, Ordinal: i}
				}
			}
		}
		x.err = fmt.Errorf("summary needs unsupported transient owner region %q", region)
	}
	return d
}

func (x *summaryExporter) facts(f *Function, facts []OwnershipFact, depth int) []summaryOwner {
	out := []summaryOwner{}
	for _, fact := range facts {
		if int(fact.ownerKind) >= len(summaryOwnerKinds) {
			x.err = fmt.Errorf("unsupported owner kind")
			return out
		}
		d := summaryOwner{Path: fact.Path, Status: fact.Status, Region: x.region(f, fact.Region), Origin: fact.Origin, SourcePath: fact.source, SourceSet: fact.sourceSet, OwnerKind: summaryOwnerKinds[fact.ownerKind], PotentialOwner: fact.potentialOwner, Remainder: fact.remainder, RemainderExclusions: fact.remainderExclusions, Relation: summaryReference{Kind: "absent"}}
		if fact.callbackRelation != nil {
			d.Relation = summaryReference{Kind: "relation", Ref: x.relation(f, fact.callbackRelation, depth+1)}
		}
		out = append(out, d)
	}
	return out
}

func (x *summaryExporter) occurrence(f *Function, value checkedExpression, depth int) string {
	ref := "occurrence:" + strconv.Itoa(len(x.dto.Occurrences))
	if depth > 32 || len(x.dto.Occurrences) >= maxInterfaceTableEntries {
		x.err = fmt.Errorf("summary occurrence budget exceeded")
		return ""
	}
	index := len(x.dto.Occurrences)
	x.dto.Occurrences = append(x.dto.Occurrences, summaryOccurrence{})
	d := summaryOccurrence{Fields: []summaryFieldOccurrence{}, Ref: ref, Contract: x.typ(value.contractID()), Ownership: x.facts(f, value.ownershipFacts(), depth), Captures: x.facts(f, value.captureFacts(), depth), Child: x.facts(f, value.child, depth), Evidence: x.callable(value.callableEvidence), Evaluation: summaryRows{Failures: x.row(value.evaluation.failureRowID()), Services: x.row(value.evaluation.serviceRowID())}, Executed: summaryRows{Failures: x.row(value.executed.failureRowID()), Services: x.row(value.executed.serviceRowID())}}
	names := []string{}
	for name := range value.fields {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		d.Fields = append(d.Fields, summaryFieldOccurrence{Name: name, Occurrence: x.occurrence(f, value.fields[name], depth+1)})
	}
	x.dto.Occurrences[index] = d
	return ref
}

func (x *summaryExporter) relation(f *Function, relation *callbackResultRelation, depth int) string {
	if x.visiting[relation] {
		x.err = fmt.Errorf("cyclic callback summary graph is unsupported")
		return ""
	}
	if ref, ok := x.relations[relation]; ok {
		return ref
	}
	if depth > 32 || len(x.relations) >= 4096 {
		x.err = fmt.Errorf("callback summary budget exceeded")
		return ""
	}
	ref := "relation:" + strconv.Itoa(len(x.relations))
	x.relations[relation] = ref
	x.visiting[relation] = true
	d := summaryRelation{Ref: ref, Evidence: x.callable(relation.callee), Arguments: []string{}, Result: x.typ(relation.result), Path: relation.path}
	for _, arg := range relation.arguments {
		d.Arguments = append(d.Arguments, x.occurrence(f, arg, depth+1))
	}
	delete(x.visiting, relation)
	x.dto.Relations = append(x.dto.Relations, d)
	return ref
}

// First reject duplicate keys, null/default manufacture, trailing bytes and
// hostile depth/width before the typed decoder allocates the complete tables.
func validateSummaryJSON(data []byte) error {
	if len(data) == 0 || len(data) > maxInterfaceSummaryBytes {
		return fmt.Errorf("interface summary byte budget exceeded")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	count := 0
	var value func(int) error
	value = func(depth int) error {
		count++
		if depth > 64 || count > 65536 {
			return fmt.Errorf("interface JSON graph budget exceeded")
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		if token == nil {
			return fmt.Errorf("null interface field is unsupported")
		}
		if delim, ok := token.(json.Delim); ok {
			switch delim {
			case '{':
				keys := map[string]bool{}
				for d.More() {
					token, err := d.Token()
					if err != nil {
						return err
					}
					key, ok := token.(string)
					if !ok || keys[key] {
						return fmt.Errorf("duplicate or invalid interface key")
					}
					keys[key] = true
					if err = value(depth + 1); err != nil {
						return err
					}
				}
			case '[':
				for d.More() {
					if err = value(depth + 1); err != nil {
						return err
					}
				}
			default:
				return fmt.Errorf("invalid interface delimiter")
			}
			_, err = d.Token()
			return err
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing interface data")
	}
	return requireSummaryFields(data, reflect.TypeFor[interfaceSummary](), 0)
}

func requireSummaryFields(data []byte, typ reflect.Type, depth int) error {
	if depth > 64 {
		return fmt.Errorf("interface field depth exceeded")
	}
	switch typ.Kind() {
	case reflect.Struct:
		fields := map[string]json.RawMessage{}
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		expected := 0
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			expected++
			value, ok := fields[name]
			if !ok {
				return fmt.Errorf("required interface field %s missing", name)
			}
			if err := requireSummaryFields(value, f.Type, depth+1); err != nil {
				return err
			}
		}
		if len(fields) != expected {
			return fmt.Errorf("unknown interface field")
		}
	case reflect.Slice:
		var values []json.RawMessage
		if err := json.Unmarshal(data, &values); err != nil {
			return err
		}
		for _, value := range values {
			if err := requireSummaryFields(value, typ.Elem(), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodeInterfaceSummary(data []byte, expectedHash, sourceInput string) (interfaceSummary, error) {
	if err := validateSummaryJSON(data); err != nil {
		return interfaceSummary{}, err
	}
	var dto interfaceSummary
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&dto); err != nil {
		return dto, err
	}
	if dto.InterfaceSchema != interfaceSummarySchema || dto.OwnershipSchema != ownershipSummarySchema || dto.SemanticABI != SemanticProducerIdentity || dto.Producer != SemanticProducerIdentity || dto.Version != bundledInterfaceVersion {
		return dto, fmt.Errorf("incompatible interface producer or schema")
	}
	if dto.ContentHash != expectedHash || dto.SourceInput != sourceInput || len(dto.Implementation) != 64 {
		return dto, fmt.Errorf("stale or incompatible distributed interface content")
	}
	copy := dto
	copy.ContentHash = ""
	canonical, err := json.Marshal(copy)
	if err != nil || formatDigest(string(canonical)) != expectedHash {
		return dto, fmt.Errorf("distributed interface digest mismatch")
	}
	if len(dto.TrustedHost) != 0 {
		return dto, fmt.Errorf("bundled host assertions are unsupported")
	}
	return dto, nil
}
