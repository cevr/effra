package compiler

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

// ProjectionLimits bound the public, response-local canonical type view. The
// private checker arena is complete regardless of these limits; refusal is a
// tooling result and never a source diagnostic.
type ProjectionLimits struct {
	Nodes              int `json:"nodes"`
	Edges              int `json:"edges"`
	RowLabels          int `json:"rowLabels"`
	NameBytes          int `json:"nameBytes"`
	CompatibilityBytes int `json:"compatibilityBytes"`
	ResponseBytes      int `json:"responseBytes"`
}

type ProjectionUsage struct {
	Nodes              int `json:"nodes"`
	Edges              int `json:"edges"`
	RowLabels          int `json:"rowLabels"`
	NameBytes          int `json:"nameBytes"`
	CompatibilityBytes int `json:"compatibilityBytes"`
	ResponseBytes      int `json:"responseBytes"`
}

// TypeProjection is a response-local complete table. References in Types and
// rows are valid only for this revision-scoped response; an incomplete result
// contains no authoritative table and carries an explicit Error.
type TypeProjection struct {
	Types    []TypeNode       `json:"types,omitempty"`
	Rows     []RowNode        `json:"rows,omitempty"`
	Limits   ProjectionLimits `json:"typeProjectionLimits"`
	Usage    ProjectionUsage  `json:"typeProjectionUsage"`
	Complete bool             `json:"typeProjectionComplete"`
	Error    string           `json:"typeProjectionError,omitempty"`
}

type canonicalSnapshot struct {
	nodes        []*semanticTypeNode
	rows         []RowNode
	publicIDs    map[TypeID]string
	publicToID   map[string]TypeID
	invalid      string
	declarations map[string]*Declaration
	labelTypes   map[string]TypeID
	rowIDs       map[string]RowID
}

const (
	maxTypeProjectionEdges              = 8192
	maxTypeProjectionRowLabels          = 4096
	maxTypeProjectionNameBytes          = 256 * 1024
	maxTypeProjectionCompatibilityBytes = 512 * 1024
	maxTypeProjectionResponseBytes      = 1024 * 1024
)

var defaultProjectionLimits = ProjectionLimits{
	Nodes:              maxTypeProjectionNodes,
	Edges:              maxTypeProjectionEdges,
	RowLabels:          maxTypeProjectionRowLabels,
	NameBytes:          maxTypeProjectionNameBytes,
	CompatibilityBytes: maxTypeProjectionCompatibilityBytes,
	ResponseBytes:      maxTypeProjectionResponseBytes,
}

func (c *checker) canonicalSnapshot() *canonicalSnapshot {
	labelTypes := map[string]TypeID{}
	for _, declaration := range c.result.Declarations {
		if declaration.Kind == "error" {
			labelTypes[declaration.Name] = c.canonicalRef(typeRef(declaration.Name))
		}
	}
	snapshot := &canonicalSnapshot{
		nodes:        c.typeNodes,
		rows:         c.rows,
		publicIDs:    map[TypeID]string{},
		publicToID:   map[string]TypeID{},
		declarations: map[string]*Declaration{},
		labelTypes:   labelTypes,
		rowIDs:       map[string]RowID{},
	}
	for i, row := range snapshot.rows {
		if _, exists := snapshot.rowIDs[row.ID]; exists {
			snapshot.invalid = "duplicate canonical row identity " + row.ID
		}
		snapshot.rowIDs[row.ID] = RowID(i + 1)
	}
	for i := range c.result.Declarations {
		d := &c.result.Declarations[i]
		if d.Kind == "template" {
			snapshot.declarations[d.Identity] = d
		} else if qualifier := c.declarationQualifier(d.Kind, d.Name); qualifier != "" {
			snapshot.declarations[qualifier] = d
		}
	}
	// Compute all public identities once at the checker-to-boundary seam. The
	// identity is memoized by the canonical interner and is never recomputed by
	// a relation or by each selected query.
	for id := TypeID(1); int(id) <= len(snapshot.nodes); id++ {
		publicID := c.typeNodeID(id)
		if publicID == "" {
			continue
		}
		snapshot.publicIDs[id] = publicID
		if previous, exists := snapshot.publicToID[publicID]; !exists || previous == id {
			snapshot.publicToID[publicID] = id
		} else {
			snapshot.invalid = "duplicate canonical type identity " + publicID
		}
	}
	return snapshot
}

func (s *canonicalSnapshot) node(id TypeID) *semanticTypeNode {
	if id == invalidTypeID || int(id) > len(s.nodes) {
		return nil
	}
	return s.nodes[id-1]
}

func (s *canonicalSnapshot) row(id RowID) (RowNode, bool) {
	if id == emptyRowID || int(id) > len(s.rows) {
		return RowNode{}, false
	}
	return s.rows[id-1], true
}

// wireNode omits Args so callers can budget them before allocating the public
// string slice. Children and rows have already been validated by the walker.
func (s *canonicalSnapshot) wireNode(id TypeID) TypeNode {
	n := s.node(id)
	w := TypeNode{ID: s.publicIDs[id], Kind: n.Kind, Name: n.Name, Declaration: n.Declaration, Mode: n.Mode}
	if n.Result != invalidTypeID {
		w.Result = s.publicIDs[n.Result]
	}
	if n.FailureRow != emptyRowID {
		w.FailureRow = s.rows[n.FailureRow-1].ID
	}
	if n.ServiceRow != emptyRowID {
		w.ServiceRow = s.rows[n.ServiceRow-1].ID
	}
	return w
}

func (r *Result) projectionLimits() ProjectionLimits {
	limits := r.TypeProjectionLimits
	if limits.Nodes == 0 {
		limits = defaultProjectionLimits
	}
	return limits
}

func refusedProjection(limits ProjectionLimits, usage ProjectionUsage, reason string) TypeProjection {
	return TypeProjection{Limits: limits, Usage: usage, Error: reason}
}

// encodedSize measures JSON without building an encoded buffer. It stops on
// the first field that crosses the limit, including compatibility metadata.
func encodedSize(value any, limit int) (int, error) {
	return encodedSizeMode(value, limit, false)
}

// encodedSizeMode can also count the JSON text embedded as a JSON string,
// which lets MCP preflight its duplicated text/structured result before
// allocating either representation.
func encodedSizeMode(value any, limit int, embedded bool) (int, error) {
	count := 0
	if embedded {
		count = 2
	}
	var walk func(reflect.Value) error
	charge := func(n int) error {
		count += n
		if count > limit {
			return fmt.Errorf("encoded response exceeds %d bytes", limit)
		}
		return nil
	}
	quoted := func(s string) error {
		quotes := 2
		if embedded {
			quotes = 4
		}
		if err := charge(quotes); err != nil {
			return err
		}
		for _, ch := range s {
			n := len(string(ch))
			switch ch {
			case '"', '\\':
				n = 2
				if embedded {
					n = 4
				}
			case '\n', '\r', '\t', '\b', '\f':
				n = 2
				if embedded {
					n = 3
				}
			case '<', '>', '&', '\u2028', '\u2029':
				n = 6
				if embedded {
					n = 7
				}
			default:
				if ch < 32 {
					n = 6
					if embedded {
						n = 7
					}
				}
			}
			if err := charge(n); err != nil {
				return err
			}
		}
		return nil
	}
	walk = func(v reflect.Value) error {
		if !v.IsValid() {
			return charge(4)
		}
		for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return charge(4)
			}
			v = v.Elem()
		}
		switch v.Kind() {
		case reflect.String:
			return quoted(v.String())
		case reflect.Bool:
			if v.Bool() {
				return charge(4)
			}
			return charge(5)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return charge(len(strconv.FormatInt(v.Int(), 10)))
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return charge(len(strconv.FormatUint(v.Uint(), 10)))
		case reflect.Float32, reflect.Float64:
			return charge(len(strconv.FormatFloat(v.Float(), 'g', -1, v.Type().Bits())))
		case reflect.Slice, reflect.Array:
			if v.Kind() == reflect.Slice && v.IsNil() {
				return charge(4)
			}
			if err := charge(2); err != nil {
				return err
			}
			for i := 0; i < v.Len(); i++ {
				if i > 0 {
					if err := charge(1); err != nil {
						return err
					}
				}
				if err := walk(v.Index(i)); err != nil {
					return err
				}
			}
		case reflect.Map:
			if v.IsNil() {
				return charge(4)
			}
			if err := charge(2); err != nil {
				return err
			}
			iter := v.MapRange()
			first := true
			for iter.Next() {
				if !first {
					if err := charge(1); err != nil {
						return err
					}
				}
				first = false
				if err := quoted(iter.Key().String()); err != nil {
					return err
				}
				if err := charge(1); err != nil {
					return err
				}
				if err := walk(iter.Value()); err != nil {
					return err
				}
			}
		case reflect.Struct:
			if err := charge(2); err != nil {
				return err
			}
			first := true
			typ := v.Type()
			for i := 0; i < v.NumField(); i++ {
				field := typ.Field(i)
				if field.PkgPath != "" {
					continue
				}
				tag := strings.Split(field.Tag.Get("json"), ",")
				if tag[0] == "-" {
					continue
				}
				f := v.Field(i)
				if slices.Contains(tag[1:], "omitempty") && (f.IsZero() || ((f.Kind() == reflect.Slice || f.Kind() == reflect.Map || f.Kind() == reflect.String) && f.Len() == 0)) {
					continue
				}
				name := tag[0]
				if name == "" {
					name = field.Name
				}
				if !first {
					if err := charge(1); err != nil {
						return err
					}
				}
				first = false
				if err := quoted(name); err != nil {
					return err
				}
				if err := charge(1); err != nil {
					return err
				}
				if err := walk(f); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("unsupported JSON projection field %s", v.Kind())
		}
		return nil
	}
	err := walk(reflect.ValueOf(value))
	return count, err
}

// ValidateMCPProjectionResponse reserves the actual outer frame and counts
// both structured JSON and its escaped content text before marshaling.
func ValidateMCPProjectionResponse(value any, frameOverhead int) error {
	limit := maxTypeProjectionResponseBytes - frameOverhead
	raw, err := encodedSize(value, limit)
	if err != nil {
		return err
	}
	_, err = encodedSizeMode(value, limit-raw, true)
	return err
}

// stringBytes charges every visible string occurrence, including duplicated
// displays and parameter names, rather than only names in the canonical table.
func stringBytes(value any, limit int) int {
	total := 0
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		if !v.IsValid() || total > limit {
			return
		}
		for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return
			}
			v = v.Elem()
		}
		switch v.Kind() {
		case reflect.String:
			total += v.Len()
		case reflect.Array, reflect.Slice:
			for i := 0; i < v.Len() && total <= limit; i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			iter := v.MapRange()
			for iter.Next() && total <= limit {
				walk(iter.Key())
				walk(iter.Value())
			}
		case reflect.Struct:
			for i := 0; i < v.NumField() && total <= limit; i++ {
				field := v.Type().Field(i)
				if field.PkgPath != "" || strings.Split(field.Tag.Get("json"), ",")[0] == "-" {
					continue
				}
				walk(v.Field(i))
			}
		}
	}
	walk(reflect.ValueOf(value))
	return total
}

// ValidateProjectionResponse charges an envelope even when its type view was
// refused. Refusal envelopes must remain bounded too.
func (r *Result) ValidateProjectionResponse(projection TypeProjection, envelope any) (ProjectionUsage, error) {
	usage := projection.Usage
	var metadata any = envelope
	switch value := envelope.(type) {
	case map[string]any:
		fields := map[string]any{}
		for key, field := range value {
			if key != "types" && key != "rows" && key != "typeProjectionUsage" && key != "typeProjectionLimits" {
				fields[key] = field
			}
		}
		metadata = fields
	case *DependencyGraph:
		metadata = struct {
			Nodes        []GraphNode
			Edges        []GraphEdge
			Declarations []Declaration
		}{value.Nodes, value.Edges, value.Declarations}
	}
	bytes, err := encodedSize(metadata, projection.Limits.CompatibilityBytes)
	if projection.Complete || bytes > usage.CompatibilityBytes {
		usage.CompatibilityBytes = bytes
	}
	if err != nil {
		return usage, fmt.Errorf("canonical compatibility metadata: %w", err)
	}
	names := stringBytes(envelope, projection.Limits.NameBytes)
	if projection.Complete || names > usage.NameBytes {
		usage.NameBytes = names
	}
	if names > projection.Limits.NameBytes {
		return usage, fmt.Errorf("canonical response strings exceed %d bytes", projection.Limits.NameBytes)
	}
	// ResponseBytes describes the final envelope, including its own decimal
	// size field. Settle that field before any adapter encodes the response.
	for attempt := 0; attempt < 8; attempt++ {
		size, err := encodedSize(envelope, projection.Limits.ResponseBytes)
		if err != nil {
			usage.ResponseBytes = size
			return usage, fmt.Errorf("canonical type projection response: %w", err)
		}
		previous := usage.ResponseBytes
		usage.ResponseBytes = size
		switch value := envelope.(type) {
		case map[string]any:
			value["typeProjectionUsage"] = usage
		case *DependencyGraph:
			value.TypeProjectionUsage = usage
		default:
			return usage, nil
		}
		if previous == size {
			return usage, nil
		}
	}
	return usage, fmt.Errorf("canonical type projection response size did not stabilize")
}

func (r *Result) projectionRefs(refs []TypeRef, all bool, compatibilityBytes int, nameBytes int) TypeProjection {
	limits := r.projectionLimits()
	usage := ProjectionUsage{CompatibilityBytes: compatibilityBytes, NameBytes: nameBytes}
	if r.canonical == nil {
		return refusedProjection(limits, usage, "canonical type projection is unavailable for this source")
	}
	if r.canonical.invalid != "" {
		return refusedProjection(limits, usage, r.canonical.invalid)
	}
	if all && len(r.canonical.nodes) > limits.Nodes {
		return refusedProjection(limits, usage, fmt.Sprintf("canonical type projection exceeds %d nodes", limits.Nodes))
	}
	if len(refs) > maxTypeProjectionEdges {
		return refusedProjection(limits, usage, "canonical projection root metadata exceeds limits")
	}
	if usage.CompatibilityBytes > limits.CompatibilityBytes {
		return refusedProjection(limits, usage, fmt.Sprintf("canonical compatibility metadata exceeds %d bytes", limits.CompatibilityBytes))
	}
	if usage.NameBytes > limits.NameBytes {
		return refusedProjection(limits, usage, fmt.Sprintf("canonical type names exceed %d bytes", limits.NameBytes))
	}

	rootIDs := []TypeID{}
	rootRows := []string{}
	rootSeen := map[TypeID]bool{}
	rowSeen := map[string]bool{}
	addType := func(publicID string) error {
		id, ok := r.canonical.publicToID[publicID]
		if !ok || r.canonical.publicIDs[id] != publicID {
			return fmt.Errorf("type reference %s is not defined in this snapshot", publicID)
		}
		if !rootSeen[id] {
			if len(rootIDs) >= limits.Nodes {
				return fmt.Errorf("canonical type projection exceeds %d nodes", limits.Nodes)
			}
			rootSeen[id] = true
			rootIDs = append(rootIDs, id)
		}
		return nil
	}
	addRoot := func(ref TypeRef) error {
		if ref.Scope != "" && ref.Scope != r.Revision {
			return fmt.Errorf("type reference belongs to a different semantic revision")
		}
		if ref.ID != "" {
			if strings.HasPrefix(ref.ID, "legacy:") {
				return fmt.Errorf("legacy type reference %s has no revision-scoped definition", ref.ID)
			}
			if err := addType(ref.ID); err != nil {
				return err
			}
		}
		for _, argID := range ref.ArgIDs {
			if err := addType(argID); err != nil {
				return err
			}
		}
		if ref.Result != "" {
			if err := addType(ref.Result); err != nil {
				return err
			}
		}
		for _, rowID := range []string{ref.FailureRow, ref.ServiceRow} {
			if rowID != "" && !rowSeen[rowID] {
				if len(rootRows) >= limits.RowLabels {
					return fmt.Errorf("canonical row projection exceeds %d labels", limits.RowLabels)
				}
				rowSeen[rowID] = true
				rootRows = append(rootRows, rowID)
			}
		}
		return nil
	}
	if all {
		rootIDs = make([]TypeID, 0, len(r.canonical.nodes))
		for id := TypeID(1); int(id) <= len(r.canonical.nodes); id++ {
			rootIDs = append(rootIDs, id)
		}
	} else {
		for _, ref := range refs {
			if err := addRoot(ref); err != nil {
				return refusedProjection(limits, usage, err.Error())
			}
		}
	}

	selected := map[TypeID]bool{}
	queue := append([]TypeID{}, rootIDs...)
	rows := map[RowID]bool{}
	queueRowDeclarations := func(row RowNode) error {
		for _, label := range row.Labels {
			if id := r.canonical.labelTypes[label]; id != invalidTypeID {
				usage.Edges++
				if usage.Edges > limits.Edges {
					return fmt.Errorf("canonical type projection exceeds %d edges", limits.Edges)
				}
				queue = append(queue, id)
			}
		}
		return nil
	}
	for _, rowID := range rootRows {
		id, found := r.canonical.rowIDs[rowID]
		if found {
			row := r.canonical.rows[id-1]
			if !rows[id] {
				usage.RowLabels += len(row.Labels) + len(row.Parameters)
				for _, p := range row.Parameters {
					usage.NameBytes += len(p.ID) + len(p.Name) + len(p.Kind) + len(p.Declaration)
				}
				usage.NameBytes += len(row.ID)
				for _, label := range row.Labels {
					usage.NameBytes += len(label)
				}
				if usage.RowLabels > limits.RowLabels || usage.NameBytes > limits.NameBytes {
					return refusedProjection(limits, usage, "canonical projection row metadata exceeds limits")
				}
				if err := queueRowDeclarations(row); err != nil {
					return refusedProjection(limits, usage, err.Error())
				}
			}
			rows[id] = true
		}
		if !found {
			return refusedProjection(limits, usage, fmt.Sprintf("row reference %s is not defined in this snapshot", rowID))
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if selected[id] {
			continue
		}
		node := r.canonical.node(id)
		if node == nil || r.canonical.publicIDs[id] == "" {
			return refusedProjection(limits, usage, fmt.Sprintf("type node %d is not defined in this snapshot", id))
		}
		if usage.Nodes >= limits.Nodes {
			return refusedProjection(limits, usage, fmt.Sprintf("canonical type projection exceeds %d nodes", limits.Nodes))
		}
		selected[id] = true
		usage.Nodes++
		publicID := r.canonical.publicIDs[id]
		usage.NameBytes += len(publicID) + len(node.Kind) + len(node.Name) + len(node.Declaration) + len(node.Mode)
		usage.CompatibilityBytes += len(publicID) + len(node.Name)
		if usage.NameBytes > limits.NameBytes {
			return refusedProjection(limits, usage, fmt.Sprintf("canonical type names exceed %d bytes", limits.NameBytes))
		}
		if usage.CompatibilityBytes > limits.CompatibilityBytes {
			return refusedProjection(limits, usage, fmt.Sprintf("canonical compatibility metadata exceeds %d bytes", limits.CompatibilityBytes))
		}
		for _, child := range node.Args {
			usage.Edges++
			usage.NameBytes += len(r.canonical.publicIDs[child])
			if usage.NameBytes > limits.NameBytes {
				return refusedProjection(limits, usage, fmt.Sprintf("canonical type names exceed %d bytes", limits.NameBytes))
			}
			if usage.Edges > limits.Edges {
				return refusedProjection(limits, usage, fmt.Sprintf("canonical type projection exceeds %d edges", limits.Edges))
			}
			if r.canonical.node(child) == nil || r.canonical.publicIDs[child] == "" {
				return refusedProjection(limits, usage, fmt.Sprintf("type node %d references an undefined child", id))
			}
			queue = append(queue, child)
		}
		if declaration := r.canonical.declarations[node.Declaration]; declaration != nil {
			if !all {
				bytes, err := encodedSize(declaration, limits.CompatibilityBytes-usage.CompatibilityBytes)
				usage.CompatibilityBytes += bytes
				if err != nil {
					return refusedProjection(limits, usage, err.Error())
				}
				usage.NameBytes += stringBytes(declaration, limits.NameBytes-usage.NameBytes)
				if usage.NameBytes > limits.NameBytes {
					return refusedProjection(limits, usage, fmt.Sprintf("canonical type names exceed %d bytes", limits.NameBytes))
				}
			}
			addField := func(field Field) error {
				usage.Edges++
				if usage.Edges > limits.Edges {
					return fmt.Errorf("canonical type projection exceeds %d edges", limits.Edges)
				}
				child := field.typeID
				if field.TypeRef.ID != r.canonical.publicIDs[child] || (field.TypeRef.Scope != "" && field.TypeRef.Scope != r.Revision) {
					return fmt.Errorf("declaration field reference disagrees with its checked snapshot")
				}
				if r.canonical.node(child) == nil || r.canonical.publicIDs[child] == "" {
					return fmt.Errorf("declaration field has no checked canonical type")
				}
				queue = append(queue, child)
				return nil
			}
			for _, field := range declaration.Fields {
				if err := addField(field); err != nil {
					return refusedProjection(limits, usage, err.Error())
				}
			}
			for _, parameter := range declaration.TemplateParameters {
				if err := addField(Field{TypeRef: parameter.Variable, typeID: parameter.typeID}); err != nil {
					return refusedProjection(limits, usage, err.Error())
				}
				if parameter.Shape != nil {
					if err := addField(Field{TypeRef: *parameter.Shape, typeID: parameter.shapeID}); err != nil {
						return refusedProjection(limits, usage, err.Error())
					}
				}
			}
			for _, variant := range declaration.Variants {
				for _, field := range variant.Fields {
					if err := addField(field); err != nil {
						return refusedProjection(limits, usage, err.Error())
					}
				}
			}
		}
		if node.Result != invalidTypeID {
			usage.Edges++
			usage.NameBytes += len(r.canonical.publicIDs[node.Result])
			if usage.NameBytes > limits.NameBytes {
				return refusedProjection(limits, usage, fmt.Sprintf("canonical type names exceed %d bytes", limits.NameBytes))
			}
			if usage.Edges > limits.Edges {
				return refusedProjection(limits, usage, fmt.Sprintf("canonical type projection exceeds %d edges", limits.Edges))
			}
			if r.canonical.node(node.Result) == nil || r.canonical.publicIDs[node.Result] == "" {
				return refusedProjection(limits, usage, fmt.Sprintf("type node %d references an undefined result", id))
			}
			queue = append(queue, node.Result)
		}
		for _, rowID := range []RowID{node.FailureRow, node.ServiceRow} {
			if rowID == emptyRowID {
				continue
			}
			usage.Edges++
			if usage.Edges > limits.Edges {
				return refusedProjection(limits, usage, fmt.Sprintf("canonical type projection exceeds %d edges", limits.Edges))
			}
			row, ok := r.canonical.row(rowID)
			if !ok {
				return refusedProjection(limits, usage, fmt.Sprintf("type node %d references an undefined row", id))
			}
			usage.NameBytes += len(row.ID)
			if usage.NameBytes > limits.NameBytes {
				return refusedProjection(limits, usage, fmt.Sprintf("canonical type names exceed %d bytes", limits.NameBytes))
			}
			if rows[rowID] {
				continue
			}
			rows[rowID] = true
			usage.RowLabels += len(row.Labels) + len(row.Parameters)
			for _, p := range row.Parameters {
				usage.NameBytes += len(p.ID) + len(p.Name) + len(p.Kind) + len(p.Declaration)
			}
			usage.NameBytes += len(row.ID)
			for _, label := range row.Labels {
				usage.NameBytes += len(label)
			}
			if usage.Edges > limits.Edges {
				return refusedProjection(limits, usage, fmt.Sprintf("canonical type projection exceeds %d edges", limits.Edges))
			}
			if usage.RowLabels > limits.RowLabels {
				return refusedProjection(limits, usage, fmt.Sprintf("canonical type projection exceeds %d row labels", limits.RowLabels))
			}
			if usage.NameBytes > limits.NameBytes {
				return refusedProjection(limits, usage, fmt.Sprintf("canonical type names exceed %d bytes", limits.NameBytes))
			}
			if err := queueRowDeclarations(row); err != nil {
				return refusedProjection(limits, usage, err.Error())
			}
		}
	}
	// Measure the wire shape from retained nodes before allocating Args or
	// cloning row labels. This is exact JSON accounting, including escapes.
	usage.ResponseBytes = len(`{"types":[],"rows":[]}`)
	for id := range selected {
		node := r.canonical.node(id)
		wire := r.canonical.wireNode(id)
		size, err := encodedSize(wire, limits.ResponseBytes-usage.ResponseBytes)
		usage.ResponseBytes += size
		if err != nil {
			return refusedProjection(limits, usage, err.Error())
		}
		if len(node.Args) > 0 {
			usage.ResponseBytes += len(`,"args":[]`)
			for i, child := range node.Args {
				if i > 0 {
					usage.ResponseBytes++
				}
				size, err := encodedSize(r.canonical.publicIDs[child], limits.ResponseBytes-usage.ResponseBytes)
				usage.ResponseBytes += size
				if err != nil {
					return refusedProjection(limits, usage, err.Error())
				}
			}
		}
	}
	if len(selected) > 0 {
		usage.ResponseBytes += len(selected) - 1
	}
	for id := range rows {
		size, err := encodedSize(r.canonical.rows[id-1], limits.ResponseBytes-usage.ResponseBytes)
		usage.ResponseBytes += size
		if err != nil {
			return refusedProjection(limits, usage, err.Error())
		}
	}
	if len(rows) > 0 {
		usage.ResponseBytes += len(rows) - 1
	}
	if usage.ResponseBytes > limits.ResponseBytes {
		return refusedProjection(limits, usage, fmt.Sprintf("canonical type projection exceeds %d response bytes", limits.ResponseBytes))
	}

	orderedIDs := make([]TypeID, 0, len(selected))
	for id := range selected {
		orderedIDs = append(orderedIDs, id)
	}
	slices.SortFunc(orderedIDs, func(a, b TypeID) int { return strings.Compare(r.canonical.publicIDs[a], r.canonical.publicIDs[b]) })
	types := make([]TypeNode, 0, len(orderedIDs))
	for _, id := range orderedIDs {
		node := r.canonical.node(id)
		args := make([]string, 0, len(node.Args))
		for _, child := range node.Args {
			args = append(args, r.canonical.publicIDs[child])
		}
		wire := r.canonical.wireNode(id)
		wire.Args = args
		types = append(types, wire)
	}
	orderedRows := make([]RowID, 0, len(rows))
	for id := range rows {
		orderedRows = append(orderedRows, id)
	}
	slices.SortFunc(orderedRows, func(a, b RowID) int { return strings.Compare(r.canonical.rows[a-1].ID, r.canonical.rows[b-1].ID) })
	projectedRows := make([]RowNode, 0, len(orderedRows))
	for _, id := range orderedRows {
		row := r.canonical.rows[id-1]
		projectedRows = append(projectedRows, RowNode{ID: row.ID, Labels: append([]string{}, row.Labels...), Parameters: append([]RowParameter(nil), row.Parameters...)})
	}
	size, err := encodedSize(struct {
		Types []TypeNode `json:"types"`
		Rows  []RowNode  `json:"rows"`
	}{types, projectedRows}, limits.ResponseBytes)
	usage.ResponseBytes = size
	if err != nil {
		return refusedProjection(limits, usage, err.Error())
	}
	return TypeProjection{Types: types, Rows: projectedRows, Limits: limits, Usage: usage, Complete: true}
}

func appendProjectionRef(refs *[]TypeRef, ref TypeRef) {
	if ref.ID != "" || len(ref.ArgIDs) > 0 || ref.Result != "" || ref.FailureRow != "" || ref.ServiceRow != "" {
		if len(*refs) <= maxTypeProjectionEdges {
			*refs = append(*refs, ref)
		}
	}
}

func appendProjectionValue(refs *[]TypeRef, value ValueType) int {
	appendProjectionRef(refs, value.Type)
	appendProjectionRef(refs, value.Contract)
	appendProjectionRef(refs, TypeRef{FailureRow: value.FailureRow, ServiceRow: value.ServiceRow})
	if value.Callable != nil {
		appendProjectionRef(refs, TypeRef{ID: value.Callable.Signature})
		for _, parameter := range value.Callable.TypeParameters {
			appendProjectionRef(refs, parameter.Variable)
			if parameter.Shape != nil {
				appendProjectionRef(refs, *parameter.Shape)
			}
		}
		for _, parameter := range value.Callable.Parameters {
			appendProjectionRef(refs, parameter.TypeRef)
		}
		appendProjectionRef(refs, value.Callable.Result)
		if value.Callable.FailureRow != "" {
			appendProjectionRef(refs, TypeRef{FailureRow: value.Callable.FailureRow})
		}
		if value.Callable.ServiceRow != "" {
			appendProjectionRef(refs, TypeRef{ServiceRow: value.Callable.ServiceRow})
		}
	}
	if value.Application != nil {
		for _, policy := range value.Application.CallbackPolicies {
			appendProjectionRef(refs, TypeRef{FailureRow: policy.FailureRow})
		}
		for _, argument := range value.Application.RowArguments {
			if argument.Row != "" {
				appendProjectionRef(refs, TypeRef{FailureRow: argument.Row})
			}
		}
		for _, argument := range value.Application.Arguments {
			appendProjectionRef(refs, argument)
		}
		appendProjectionRef(refs, value.Application.Result)
	}
	for _, labels := range [][]string{value.Evaluation.Failures, value.Evaluation.Requirements} {
		if len(labels) > 0 {
			appendProjectionRef(refs, TypeRef{FailureRow: rowNodeIDForLabels(labels)})
		}
	}
	return 0
}

func (r *Result) ProjectAllTypes() TypeProjection {
	if r.publicationRefused {
		return refusedProjection(r.projectionLimits(), r.publicationUsage, "whole-source compatibility projection exceeds limits")
	}
	for _, symbol := range r.Symbols {
		if symbol.Contract.ProjectionError != "" || symbol.Actual.ProjectionError != "" {
			return refusedProjection(r.projectionLimits(), ProjectionUsage{}, "whole-source compatibility projection exceeds limits")
		}
	}
	metadata := struct {
		Symbols      []Symbol
		Declarations []Declaration
		Bindings     []Binding
	}{r.Symbols, r.Declarations, r.Bindings}
	compatibilityBytes, _ := encodedSize(metadata, r.projectionLimits().CompatibilityBytes)
	return r.projectionRefs(nil, true, compatibilityBytes, stringBytes(metadata, r.projectionLimits().NameBytes))
}

// CheckResponse separates source admission from inspection availability. A
// refused envelope retains counts and diagnostics, but no unresolved metadata
// references. Both CLI and MCP use this same authority boundary.
func (r *Result) CheckResponse() map[string]any {
	projection := r.ProjectAllTypes()
	if !r.Checked {
		projection = refusedProjection(r.projectionLimits(), ProjectionUsage{}, "type projection requires checked source")
	}
	diagnostics := r.Diagnostics
	truncated := len(diagnostics) > 100
	if truncated {
		diagnostics = diagnostics[:100]
	}
	response := map[string]any{
		"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "checked": r.Checked,
		"diagnostics": diagnostics, "diagnosticsTruncated": truncated, "symbolCount": len(r.Symbols), "declarationCount": len(r.Declarations),
		"timings": r.Timings, "typeProjectionBudget": r.TypeProjectionBudget, "typeProjectionLimits": projection.Limits,
		"producerIdentity":    r.ProducerIdentity,
		"typeProjectionUsage": projection.Usage, "typeProjectionComplete": projection.Complete,
	}
	if projection.Complete {
		response["symbols"] = r.Symbols
		response["declarations"] = r.Declarations
		response["bindings"] = r.Bindings
		response["types"] = projection.Types
		response["rows"] = projection.Rows
		response["sources"] = r.Sources
		response["bundledBindings"] = r.BundledBindings
		response["bundledInterfaces"] = r.BundledInterfaces
		usage, err := r.ValidateProjectionResponse(projection, response)
		if err != nil {
			projection = refusedProjection(projection.Limits, usage, err.Error())
			for _, key := range []string{"symbols", "declarations", "bindings", "types", "rows", "sources", "bundledBindings", "bundledInterfaces"} {
				delete(response, key)
			}
			response["typeProjectionComplete"] = false
		} else {
			projection.Usage = usage
		}
	}
	response["typeProjectionUsage"] = projection.Usage
	if !projection.Complete {
		response["typeProjectionError"] = projection.Error
	}
	// Source diagnostics themselves may be large. Refuse the entire operational
	// response rather than silently dropping a diagnostic message.
	if _, err := r.ValidateProjectionResponse(projection, response); err != nil {
		return map[string]any{"schemaVersion": r.SchemaVersion, "revision": r.Revision, "target": r.Target, "checked": r.Checked, "symbolCount": len(r.Symbols), "declarationCount": len(r.Declarations), "typeProjectionComplete": false, "typeProjectionError": err.Error(), "diagnosticsUnavailable": true}
	}
	return response
}

func (r *Result) ProjectValue(value ValueType) TypeProjection {
	return r.projectValues([]ValueType{value}, 0)
}

func (r *Result) ProjectValues(values []ValueType) TypeProjection {
	return r.projectValues(values, 0)
}

func (r *Result) projectValues(values []ValueType, compatibilityBytes int) TypeProjection {
	for _, value := range values {
		if value.ProjectionError != "" {
			return refusedProjection(r.projectionLimits(), ProjectionUsage{}, value.ProjectionError)
		}
	}
	size, err := encodedSize(values, r.projectionLimits().CompatibilityBytes)
	if err != nil {
		return refusedProjection(r.projectionLimits(), ProjectionUsage{CompatibilityBytes: size}, err.Error())
	}
	refs := []TypeRef{}
	compatibilityBytes += size
	for _, value := range values {
		compatibilityBytes += appendProjectionValue(&refs, value)
	}
	return r.projectionRefs(refs, false, compatibilityBytes, stringBytes(values, r.projectionLimits().NameBytes))
}

func (r *Result) ProjectSymbol(symbol *Symbol) TypeProjection {
	if symbol == nil {
		return refusedProjection(r.projectionLimits(), ProjectionUsage{}, "symbol projection is unavailable")
	}
	if symbol.Contract.ProjectionError != "" || symbol.Actual.ProjectionError != "" {
		return refusedProjection(r.projectionLimits(), ProjectionUsage{}, "symbol compatibility projection exceeds limits")
	}
	refs := []TypeRef{}
	compatibilityBytes, err := encodedSize(symbol, r.projectionLimits().CompatibilityBytes)
	if err != nil {
		return refusedProjection(r.projectionLimits(), ProjectionUsage{CompatibilityBytes: compatibilityBytes}, err.Error())
	}
	checked, ok := r.checkedSymbols[symbol.Identity]
	if !ok || r.projector == nil {
		return refusedProjection(r.projectionLimits(), ProjectionUsage{}, "symbol has no retained checked roots")
	}
	appendProjectionRef(&refs, r.projector.identityRef(checked.contract.contractID()))
	appendProjectionRef(&refs, r.projector.identityRef(checked.body.contractID()))
	for _, parameter := range checked.declaration.Params {
		appendProjectionRef(&refs, r.projector.identityRef(parameter.typeID))
	}
	// The retained IDs choose authority; validate every compatibility reference
	// that will also cross the boundary, so an internal projection mismatch
	// cannot turn a complete canonical table into a dangling response.
	for _, parameter := range symbol.Params {
		appendProjectionRef(&refs, parameter.TypeRef)
	}
	appendProjectionValue(&refs, symbol.Contract)
	appendProjectionValue(&refs, symbol.Actual)
	return r.projectionRefs(refs, false, compatibilityBytes, stringBytes(symbol, r.projectionLimits().NameBytes))
}

// ProjectTestCatalog admits all selected compatibility roots cumulatively
// before Find expands any refused whole-source views. Discovery publication
// never changes stored symbols, whole-source refusal or executable test roots.
func (r *Result) ProjectTestCatalog(tests []*Symbol) ([]*Symbol, TypeProjection) {
	limits := r.projectionLimits()
	refuse := func(bytes int, reason string) ([]*Symbol, TypeProjection) {
		return nil, refusedProjection(limits, ProjectionUsage{CompatibilityBytes: bytes}, reason)
	}
	if !r.Checked || r.projector == nil {
		return refuse(0, "test catalog requires retained checked source")
	}
	compatibilityBytes := 2 // JSON array brackets.
	refs := []TypeRef{}
	for i, symbol := range tests {
		if symbol == nil {
			return refuse(compatibilityBytes, "test catalog has no checked symbol")
		}
		checked, ok := r.checkedSymbols[symbol.Identity]
		if !ok {
			return refuse(compatibilityBytes, "test catalog has no retained checked roots")
		}
		if i > 0 {
			compatibilityBytes++
		}
		size, err := r.projector.checkedSymbolSize(*symbol, checked, limits.CompatibilityBytes-compatibilityBytes)
		compatibilityBytes += size
		if err != nil {
			return refuse(compatibilityBytes, err.Error())
		}
		appendProjectionRef(&refs, r.projector.identityRef(checked.contract.contractID()))
		appendProjectionRef(&refs, r.projector.identityRef(checked.body.contractID()))
	}
	// Admit the union of checked roots and nominal closure before materializing
	// selected callable metadata. No per-test projection resets this budget.
	projection := r.projectionRefs(refs, false, compatibilityBytes, 0)
	if !projection.Complete {
		return nil, projection
	}
	selected := make([]*Symbol, 0, len(tests))
	for _, symbol := range tests {
		view := r.Find(symbol.Name)
		if view == nil || view.Contract.ProjectionError != "" || view.Actual.ProjectionError != "" {
			return refuse(compatibilityBytes, "selected test compatibility projection is unavailable")
		}
		selected = append(selected, view)
		appendProjectionValue(&refs, view.Contract)
		appendProjectionValue(&refs, view.Actual)
	}
	projection = r.projectionRefs(refs, false, compatibilityBytes, stringBytes(selected, limits.NameBytes))
	if !projection.Complete {
		return nil, projection
	}
	return selected, projection
}

func (r *Result) ProjectExpression(info *ExpressionInfo) TypeProjection {
	if info == nil {
		return refusedProjection(r.projectionLimits(), ProjectionUsage{}, "expression projection is unavailable")
	}
	if info.Type.ProjectionError != "" {
		return refusedProjection(r.projectionLimits(), ProjectionUsage{}, info.Type.ProjectionError)
	}
	refs := []TypeRef{}
	compatibilityBytes, _ := encodedSize(info, r.projectionLimits().CompatibilityBytes)
	appendProjectionValue(&refs, info.Type)
	for _, labels := range [][]string{info.Evaluation.Failures, info.Evaluation.Requirements, info.ExecutedFailures, info.ExecutedRequirements, info.Type.Evaluation.Failures, info.Type.Evaluation.Requirements} {
		if len(labels) == 0 {
			continue
		}
		rowID := rowNodeIDForLabels(labels)
		refs = append(refs, TypeRef{FailureRow: rowID})
	}
	return r.projectionRefs(refs, false, compatibilityBytes, stringBytes(info, r.projectionLimits().NameBytes))
}

func (r *Result) ProjectDeclaration(declaration *Declaration) TypeProjection {
	if declaration == nil {
		return refusedProjection(r.projectionLimits(), ProjectionUsage{}, "declaration projection is unavailable")
	}
	refs := []TypeRef{}
	compatibilityBytes, err := encodedSize(declaration, r.projectionLimits().CompatibilityBytes)
	if err != nil {
		return refusedProjection(r.projectionLimits(), ProjectionUsage{CompatibilityBytes: compatibilityBytes}, err.Error())
	}
	for _, field := range declaration.Fields {
		appendProjectionRef(&refs, field.TypeRef)
	}
	for _, parameter := range declaration.TemplateParameters {
		appendProjectionRef(&refs, parameter.Variable)
		if parameter.Shape != nil {
			appendProjectionRef(&refs, *parameter.Shape)
		}
	}
	for _, variant := range declaration.Variants {
		for _, field := range variant.Fields {
			appendProjectionRef(&refs, field.TypeRef)
		}
	}
	return r.projectionRefs(refs, false, compatibilityBytes, stringBytes(declaration, r.projectionLimits().NameBytes))
}

// ProjectionDeclarations returns only nominal definitions in this complete
// response-local closure. The retained declarations are immutable after check.
func (r *Result) ProjectionDeclarations(projection TypeProjection) []Declaration {
	if !projection.Complete {
		return nil
	}
	selected := map[string]bool{}
	for _, node := range projection.Types {
		selected[node.Kind+":"+node.Name] = true
	}
	declarations := []Declaration{}
	for _, declaration := range r.Declarations {
		if selected[declaration.Kind+":"+declaration.Name] {
			declarations = append(declarations, declaration)
		}
	}
	return declarations
}

// SymbolBindings describes host calls in the selected function and its checked
// named call closure.
// Unrelated imported signatures do not enlarge symbol inspection envelopes.
func (r *Result) SymbolBindings(symbol *Symbol) ([]Binding, error) {
	if len(r.Bindings) == 0 {
		return []Binding{}, nil
	}
	selected := map[string]bool{}
	visited := map[*Function]bool{}
	var traversalErr error
	var block func(*Block)
	var expr func(*Expr)
	var function func(*Function)
	function = func(f *Function) {
		if f == nil || visited[f] || traversalErr != nil {
			return
		}
		if len(visited) >= maxTypeProjectionEdges {
			traversalErr = fmt.Errorf("selected host-binding call closure exceeds limits")
			return
		}
		visited[f] = true
		block(f.Body)
	}
	expr = func(e *Expr) {
		if e == nil || traversalErr != nil {
			return
		}
		if e.Text == "foreign" {
			selected[e.Name] = true
		}
		if e.checked.application != nil {
			function(r.projector.functions[e.checked.application.Callee])
		}
		if e.ResolvedFunction != nil {
			function(e.ResolvedFunction)
		}
		forEachExprChild(e, expr)
		block(e.Then)
		block(e.Else)
		for _, arm := range e.Arms {
			block(arm.Body)
		}
	}
	block = func(b *Block) {
		if b != nil {
			for _, s := range b.Statements {
				expr(s.Value)
				expr(s.Payload)
			}
		}
	}
	for _, f := range r.Program.Functions {
		if f.Identity != symbol.Identity {
			continue
		}
		function(f)
	}
	if traversalErr != nil {
		return nil, traversalErr
	}
	bindings := []Binding{}
	for _, binding := range r.Bindings {
		if selected[binding.Symbol] {
			bindings = append(bindings, binding)
		}
	}
	return bindings, nil
}

// referenceSize counts the exact wire representation directly from an arena
// node. No public Args/ArgIDs slice is allocated during this preflight.
func (c *checker) referenceSize(id TypeID, limit int) (int, error) {
	n := c.node(id)
	if n == nil {
		return encodedSize(TypeRef{Kind: "invalid"}, limit)
	}
	ref := c.identityRef(id)
	if n.Result != invalidTypeID {
		ref.Result = c.typeNodeID(n.Result)
	}
	ref.FailureRow = c.rowNodeID(n.FailureRow)
	ref.ServiceRow = c.rowNodeID(n.ServiceRow)
	size, err := encodedSize(ref, limit)
	if err != nil {
		return size, err
	}
	if len(n.Args) > 0 {
		size += len(`,"args":[]`)
		for i, child := range n.Args {
			if i > 0 {
				size++
			}
			n, err := encodedSize(c.typeNodeID(child), limit-size)
			size += n
			if err != nil {
				return size, err
			}
		}
	}
	if size > limit {
		return size, fmt.Errorf("compatibility metadata exceeds %d bytes", limit)
	}
	return size, nil
}

func (c *checker) checkedCompatibilitySize(e checkedExpression, base ValueType, limit int) (int, error) {
	size, err := encodedSize(base, limit)
	if err != nil {
		return size, err
	}
	shapeID := e.resultID()
	if e.kind() == checkedFiberValue {
		shapeID = e.contractID()
	}
	for _, id := range []TypeID{shapeID, e.contractID()} {
		old, _ := encodedSize(c.identityRef(id), limit)
		full, err := c.referenceSize(id, limit-size+old)
		size += full - old
		if err != nil {
			return size, err
		}
	}
	if e.callableDecl != nil {
		f := e.callableDecl
		kind := "pure"
		if f.Effect {
			kind = "effect"
		}
		callable := CallableType{ID: f.Identity, Signature: c.typeNodeID(e.contractID()), Kind: kind, Result: c.identityRef(e.resultID()), Failures: c.retainedRowLabels(e.failureRow()), Requirements: c.retainedRowLabels(e.serviceRow()), RowParameters: f.RowParameters, CallbackPolicies: f.CallbackPolicies}
		n, err := encodedSize(callable, limit-size)
		size += len(`,"callable":`) + n
		if err != nil {
			return size, err
		}
		old, _ := encodedSize(callable.Result, limit)
		full, err := c.referenceSize(e.resultID(), limit-size+old)
		size += full - old
		if err != nil {
			return size, err
		}
		// Replace the null parameter list in the lightweight shape with the
		// already retained checked declaration parameters, without copying them.
		n, err = encodedSize(f.Params, limit-size+4)
		size += n - 4
		if err != nil {
			return size, err
		}
	}
	if e.application != nil {
		n, err := encodedSize(e.application, limit-size)
		size += len(`,"application":`) + n
		if err != nil {
			return size, err
		}
	}
	if size > limit {
		return size, fmt.Errorf("compatibility metadata exceeds %d bytes", limit)
	}
	return size, nil
}

// checkedSymbolSize charges an expanded symbol before allocating its callable
// parameters or structural reference arrays. The prototype borrows source data.
func (c *checker) checkedSymbolSize(prototype Symbol, checked checkedSymbol, limit int) (int, error) {
	prototype.Contract = c.projectCheckedBase(checked.contract)
	prototype.Actual = c.projectCheckedBase(checked.body)
	prototype.Params = checked.declaration.Params
	prototype.Contributions = checked.contributions
	size, err := encodedSize(prototype, limit)
	if err != nil {
		return size, err
	}
	for _, part := range []struct {
		fact checkedExpression
		base ValueType
	}{{checked.contract, prototype.Contract}, {checked.body, prototype.Actual}} {
		old, _ := encodedSize(part.base, limit)
		full, err := c.checkedCompatibilitySize(part.fact, part.base, limit-size+old)
		size += full - old
		if err != nil {
			return size, err
		}
	}
	return size, nil
}
