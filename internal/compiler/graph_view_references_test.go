package compiler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// validateGraphViewReferences walks the typed view instead of a JSON round
// trip. The two agree only where the typed view is what encoding/json
// publishes; these tests pin where they differ and that the difference
// carries no reference (round-2 review m1).

// graphViewCheckedKeys are the JSON keys validateGraphViewReferences resolves.
var graphViewCheckedKeys = []string{"ref", "signature", "failureRow", "serviceRow", "row", "result", "args", "labels", "source"}

// graphJSONKeys lists the JSON keys reachable from typ through exported
// fields, as encoding/json names them: an untagged embedded struct is
// flattened, a "-" tag is skipped.
func graphJSONKeys(typ reflect.Type, seen map[reflect.Type]bool, keys map[string]bool) {
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array || typ.Kind() == reflect.Map {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || seen[typ] {
		return
	}
	seen[typ] = true
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag := field.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" || (!field.IsExported() && !field.Anonymous) {
			continue
		}
		if field.Anonymous && name == "" {
			graphJSONKeys(field.Type, seen, keys)
			continue
		}
		if name == "" {
			name = field.Name
		}
		keys[name] = true
		graphJSONKeys(field.Type, seen, keys)
	}
}

// graphViewMarshalers lists every type reachable from GraphView that
// implements json.Marshaler by value or by pointer.
func graphViewMarshalers() []string {
	marshaler := reflect.TypeFor[json.Marshaler]()
	seen := map[reflect.Type]bool{}
	found := map[string]bool{}
	var visit func(reflect.Type)
	visit = func(typ reflect.Type) {
		if seen[typ] {
			return
		}
		seen[typ] = true
		if typ.Kind() != reflect.Pointer && (typ.Implements(marshaler) || reflect.PointerTo(typ).Implements(marshaler)) {
			found[typ.String()] = true
		}
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			visit(typ.Elem())
		case reflect.Map:
			visit(typ.Key())
			visit(typ.Elem())
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				if field := typ.Field(i); field.IsExported() || field.Anonymous {
					visit(field.Type)
				}
			}
		}
	}
	visit(reflect.TypeFor[GraphView]())
	out := make([]string, 0, len(found))
	for name := range found {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func TestGraphViewReferenceWalkSeesEveryPublishedKey(t *testing.T) {
	// ValueType is the only type reachable from GraphView whose published
	// form is not its struct fields.
	if got := graphViewMarshalers(); !slices.Equal(got, []string{"compiler.ValueType"}) {
		t.Fatalf("json.Marshaler types reachable from GraphView = %v; the reference walk must follow each published form", got)
	}
	// ValueType publishes its ownership facts as PublicOwnership where the
	// walk sees OwnershipFact. Neither form carries a key the walk checks, so
	// the walk and the published view resolve the same references.
	for _, typ := range []reflect.Type{reflect.TypeFor[PublicOwnership](), reflect.TypeFor[OwnershipFact]()} {
		keys := map[string]bool{}
		graphJSONKeys(typ, map[reflect.Type]bool{}, keys)
		for _, key := range graphViewCheckedKeys {
			if keys[key] {
				t.Fatalf("%s publishes checked key %q", typ, key)
			}
		}
	}
}

// graphRoundTripReferences is validateGraphViewReferences as it was before
// the typed walk (78b1ed5d): resolve every reference over the JSON round trip
// of the view. It is the reference the typed walk is compared with.
func graphRoundTripReferences(view *GraphView) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	encoded, err := json.Marshal(view)
	if err != nil {
		return err
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		return err
	}
	facts := view.Data.Effra.Facts
	types, rows, declarations, sources, parameters := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, node := range facts.Types {
		types[node.ID] = true
	}
	for _, row := range facts.Rows {
		rows[row.ID] = true
		for _, parameter := range row.Parameters {
			parameters[parameter.ID] = true
		}
	}
	for _, declaration := range facts.Declarations {
		declarations[declaration.Identity] = true
	}
	for _, source := range facts.Sources {
		sources[source.ID] = true
	}
	for _, node := range facts.Types {
		if node.Declaration != "" && !declarations[node.Declaration] && !qualifiedDeclarationPublished(node.Declaration, facts.Declarations) {
			return fmt.Errorf("type %s references unpublished declaration %s", node.ID, node.Declaration)
		}
	}
	var walk func(key string, value any) error
	walk = func(key string, value any) error {
		switch value := value.(type) {
		case []any:
			for _, item := range value {
				if err := walk(key, item); err != nil {
					return err
				}
			}
			for _, item := range value {
				id, ok := item.(string)
				if ok && key == "args" && !types[id] {
					return fmt.Errorf("unpublished type reference args=%q", id)
				}
				if ok && key == "labels" && strings.HasPrefix(id, "row-parameter:") && !parameters[id] {
					return fmt.Errorf("unpublished row parameter %q", id)
				}
			}
		case map[string]any:
			for child, item := range value {
				if err := walk(child, item); err != nil {
					return err
				}
			}
		case string:
			if value == "" {
				return nil
			}
			switch key {
			case "ref", "signature", "result":
				if !types[value] {
					return fmt.Errorf("unpublished type reference %s=%q", key, value)
				}
			case "failureRow", "serviceRow", "row":
				if !rows[value] {
					return fmt.Errorf("unpublished row reference %s=%q", key, value)
				}
			}
		}
		return nil
	}
	data := wire["data"].(map[string]any)["effra"].(map[string]any)
	tables := data["facts"].(map[string]any)
	if err := walk("types", tables["types"]); err != nil {
		return err
	}
	if err := walk("rows", tables["rows"]); err != nil {
		return err
	}
	for _, collection := range []string{"nodes", "edges"} {
		items, _ := wire[collection].([]any)
		for _, item := range items {
			entity := item.(map[string]any)["data"].(map[string]any)["effra"].(map[string]any)
			if source, ok := entity["source"].(string); ok && source != "" && !sources[source] {
				return fmt.Errorf("unpublished source reference %q", source)
			}
			if err := walk(collection, entity); err != nil {
				return err
			}
		}
	}
	return nil
}

// graphReviewViews are every view the examples and a few machine shapes
// publish, keyed by example, target and kind.
func graphReviewViews(t *testing.T) map[string]*GraphView {
	t.Helper()
	views := map[string]*GraphView{}
	files, err := filepath.Glob(filepath.Join("..", "..", "examples", "*.ef"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		r := CompileAt(string(raw), "go", filepath.Dir(file))
		if !r.Checked {
			continue
		}
		for _, kind := range []string{"dependency", "layers", "application", "machine"} {
			request, err := ParseGraphRequest(map[string]any{"kind": kind})
			if err != nil {
				continue
			}
			if view, err := r.GraphView(request); err == nil {
				views[fmt.Sprintf("%s/go/%s", filepath.Base(file), kind)] = view
			}
		}
	}
	request, err := ParseGraphRequest(map[string]any{"kind": "machine"})
	if err != nil {
		t.Fatal(err)
	}
	for i, shape := range []machineShape{
		{states: 2, events: 1, failures: 3, declarations: 4},
		{states: 2, events: 1, name: 64, payload: 3},
		{states: 1, events: 1, generic: true, genericName: 16, declarations: 2},
		{states: 6, events: 2, helpers: 2, tails: 3, wideArm: true},
	} {
		if view, err := Compile(shape.source()).GraphView(request); err == nil {
			views[fmt.Sprintf("machine shape %d", i)] = view
		}
	}
	return views
}

// graphViewStrings collects every settable string reachable through exported
// fields, slices, arrays and pointers.
func graphViewStrings(v reflect.Value, out *[]reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			graphViewStrings(v.Elem(), out)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			graphViewStrings(v.Index(i), out)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				graphViewStrings(v.Field(i), out)
			}
		}
	case reflect.String:
		if v.CanSet() {
			*out = append(*out, v)
		}
	}
}

// The typed walk and the round trip agree on every published view and on
// the same views with one string replaced by a dangling reference (the
// round-2 differential, sampled).
func TestGraphViewReferenceWalkMatchesTheRoundTrip(t *testing.T) {
	views := graphReviewViews(t)
	if len(views) < 50 {
		t.Fatalf("only %d views", len(views))
	}
	mutations, rejected := 0, 0
	for name, view := range views {
		if old, typed := graphRoundTripReferences(view), validateGraphViewReferences(view); (old == nil) != (typed == nil) {
			t.Fatalf("%s: round trip %v, typed walk %v", name, old, typed)
		}
		var strs []reflect.Value
		graphViewStrings(reflect.ValueOf(view), &strs)
		stride := max(1, len(strs)/40)
		for i := 0; i < len(strs); i += stride {
			s := strs[i]
			original := s.String()
			for _, replacement := range []string{"zz-dangling", "row-parameter:zz-dangling"} {
				s.SetString(replacement)
				old, typed := graphRoundTripReferences(view), validateGraphViewReferences(view)
				mutations++
				if typed != nil {
					rejected++
				}
				if (old == nil) != (typed == nil) {
					s.SetString(original)
					t.Fatalf("%s, string %d (%q -> %q): round trip %v, typed walk %v", name, i, original, replacement, old, typed)
				}
			}
			s.SetString(original)
		}
	}
	if rejected == 0 {
		t.Fatalf("%d mutations, none rejected", mutations)
	}
}

// The response check measures what encoding/json publishes for every machine
// view. (Dependency views carrying ValueType ownership are measured on the
// typed fields; that shared projection follow-up is not machine code.)
func TestMachineGraphViewEncodedSizeIsExact(t *testing.T) {
	for name, view := range graphReviewViews(t) {
		if view.Data.Effra.Kind != GraphKindMachine {
			continue
		}
		measured, err := encodedSize(view, 1<<30)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		encoded, err := json.Marshal(view)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if measured != len(encoded) {
			t.Fatalf("%s: encodedSize %d, encoding/json %d", name, measured, len(encoded))
		}
	}
}
