package compiler

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	rt "effra.local/prototype/runtime/effra"
)

// codecVectorJob is one vector the generated Go harness runs: a decode body
// (decoded, then re-encoded) or an encode value.
type codecVectorJob struct {
	id, plan string
	body     []byte
	value    any
	decode   bool
}

// runCodecVectorsGenerated runs vectors on the native Go engine's generated
// path. Each plan becomes generated Go declarations shaped as the Go backend
// shapes Effra data (records as structs holding nested records inline,
// unions as interfaces over variant structs), Construct adapters, and the
// writer goCodecWriters emits for compiler-derived plans; values become Go
// literals. Transform nodes run the named target-neutral behaviors over
// string domains. One program runs every job and prints one outcome per job.
func runCodecVectorsGenerated(t *testing.T, profile string, plans map[string]codecVectorPlan, jobs []codecVectorJob) map[string]codecOutcome {
	t.Helper()
	names := make([]string, 0, len(plans))
	for name := range plans {
		names = append(names, name)
	}
	sort.Strings(names)
	index := map[string]int{}
	var program strings.Builder
	program.WriteString(`package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	er "effra.generated/runtime"
)

var _ = strings.ToLower

type stepFailure struct{ step, message string }

func (f *stepFailure) Error() string { return f.step + ": " + f.message }

type outcome struct {
	ID      string   ` + "`json:\"id\"`" + `
	OK      bool     ` + "`json:\"ok\"`" + `
	Encoded string   ` + "`json:\"encoded\"`" + `
	Reason  string   ` + "`json:\"reason\"`" + `
	Path    []string ` + "`json:\"path\"`" + `
	Offset  int      ` + "`json:\"offset\"`" + `
	Step    string   ` + "`json:\"step\"`" + `
	Message string   ` + "`json:\"message\"`" + `
}

var lines = json.NewEncoder(os.Stdout)

func settle(id, stage string, output []byte, err error) {
	var failure *er.CodecError
	var step *stepFailure
	line := outcome{ID: id, OK: true, Encoded: string(output)}
	switch {
	case errors.As(err, &failure):
		line = outcome{ID: id, Reason: stage + string(failure.Reason), Path: failure.Path, Offset: failure.Offset}
	case errors.As(err, &step):
		line = outcome{ID: id, Step: stage + step.step, Message: step.message}
	case err != nil:
		panic(err)
	}
	if err := lines.Encode(line); err != nil {
		panic(err)
	}
}

func compile(plan er.CodecPlan) *er.Codec {
	codec, err := er.CompileCodec(plan)
	if err != nil {
		panic(err)
	}
	return codec
}
`)
	for position, name := range names {
		index[name] = position
		program.WriteString(codecVectorGoDeclarations(t, profile, position, plans[name]))
	}
	program.WriteString("func main() {\n")
	for _, job := range jobs {
		plan, position := plans[job.plan], index[job.plan]
		prefix := "p" + strconv.Itoa(position)
		root := codecVectorGoType(position, plan, plan.Root)
		encode := "efCodecEncode_" + strconv.Itoa(position)
		if job.decode {
			fmt.Fprintf(&program, "{\nvalue, err := %s_plan.DecodeSteps([]byte(%s), %s_decode)\nif err != nil {\nsettle(%q, \"\", nil, err)\n} else {\ndecoded := value.(%s)\noutput, err := %s(&decoded)\nsettle(%q, \"re-encode \", output, err)\n}\n}\n", prefix, strconv.Quote(string(job.body)), prefix, job.id, root, encode, job.id)
			continue
		}
		fmt.Fprintf(&program, "{\nvalue := %s(%s)\noutput, err := %s(&value)\nsettle(%q, \"\", output, err)\n}\n", root, codecVectorGoLiteral(t, position, plan, plan.Root, job.value), encode, job.id)
	}
	program.WriteString("}\n")

	dir := t.TempDir()
	sources, err := rt.SelectSources(rt.RuntimeModuleCodec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "runtime"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range sources {
		if err := os.WriteFile(filepath.Join(dir, "runtime", name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{"go.mod": "module " + generatedModulePath + "\n\ngo 1.27\n", "main.go": program.String()}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	command := exec.Command("go", "run", ".")
	command.Dir = dir
	command.Env = generatedGoEnvironment()
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("generated vector harness: %v\n%s", err, stderr.String())
	}
	results := map[string]codecOutcome{}
	decoder := json.NewDecoder(&stdout)
	for decoder.More() {
		var line struct {
			ID string `json:"id"`
			codecOutcome
		}
		if err := decoder.Decode(&line); err != nil {
			t.Fatal(err)
		}
		results[line.ID] = line.codecOutcome
	}
	if len(results) != len(jobs) {
		t.Fatalf("generated vector harness reported %d of %d jobs", len(results), len(jobs))
	}
	return results
}

func codecVectorGoName(position, node int) string {
	return "p" + strconv.Itoa(position) + "n" + strconv.Itoa(node)
}

func codecVectorGoVariant(position, node, variant int) string {
	return codecVectorGoName(position, node) + "v" + strconv.Itoa(variant)
}

// codecVectorGoType is a node's generated Go carrier; a transform node
// carries its behavior's domain, a string.
func codecVectorGoType(position int, plan codecVectorPlan, node int) string {
	switch plan.Nodes[node].Kind {
	case "string", "transform":
		return "string"
	case "bool":
		return "bool"
	case "i64":
		return "int64"
	case "void":
		return "struct{}"
	}
	return codecVectorGoName(position, node)
}

// codecVectorGoDeclarations renders one plan's carrier declarations,
// Construct adapters, compiled plan, steps and generated writer.
func codecVectorGoDeclarations(t *testing.T, profile string, position int, plan codecVectorPlan) string {
	t.Helper()
	prefix := "p" + strconv.Itoa(position)
	var out strings.Builder
	structFields := func(fields []codecVectorField) string {
		var body strings.Builder
		for i, field := range fields {
			fmt.Fprintf(&body, "F%d %s\n", i, codecVectorGoType(position, plan, field.Node))
		}
		return body.String()
	}
	construct := func(goType string, fields []codecVectorField) string {
		values := []string{}
		for i, field := range fields {
			values = append(values, fmt.Sprintf("F%d: fields[%d].(%s)", i, i, codecVectorGoType(position, plan, field.Node)))
		}
		return goType + "{" + strings.Join(values, ", ") + "}"
	}
	planFields := func(fields []codecVectorField) string {
		values := []string{}
		for _, field := range fields {
			values = append(values, fmt.Sprintf("{Name: %q, Node: %d}", field.Name, field.Node))
		}
		return "[]er.CodecField{" + strings.Join(values, ", ") + "}"
	}
	writerFields := func(fields []codecVectorField) []goCodecWriterField {
		out := []goCodecWriterField{}
		for i, field := range fields {
			out = append(out, goCodecWriterField{Name: field.Name, GoField: "F" + strconv.Itoa(i), Node: field.Node})
		}
		return out
	}
	nodes, writers := []string{}, []goCodecWriterNode{}
	steps := map[string][]string{"decode": {}, "encode": {}}
	for index, node := range plan.Nodes {
		goType := codecVectorGoType(position, plan, index)
		literal := fmt.Sprintf("{Kind: er.CodecKind(%q)", node.Kind)
		writer := goCodecWriterNode{Kind: rt.CodecKind(node.Kind), GoType: goType}
		switch node.Kind {
		case "record":
			fmt.Fprintf(&out, "type %s struct {\n%s}\n", goType, structFields(node.Fields))
			literal += fmt.Sprintf(", Type: %q, Fields: %s, Construct: func(_ int, fields []any) any { return %s }", node.Type, planFields(node.Fields), construct(goType, node.Fields))
			writer.Fields = writerFields(node.Fields)
		case "union":
			fmt.Fprintf(&out, "type %s interface{ is%s() }\n", goType, goType)
			variants, cases := []string{}, []string{}
			for v, variant := range node.Variants {
				variantType := codecVectorGoVariant(position, index, v)
				fmt.Fprintf(&out, "type %s struct {\n%s}\nfunc (%s) is%s() {}\n", variantType, structFields(variant.Fields), variantType, goType)
				variants = append(variants, fmt.Sprintf("{Tag: %q, Fields: %s}", variant.Tag, planFields(variant.Fields)))
				cases = append(cases, fmt.Sprintf("case %d: return %s(%s)\n", v, goType, construct(variantType, variant.Fields)))
				writer.Variants = append(writer.Variants, goCodecWriterVariant{Tag: variant.Tag, GoType: variantType, Fields: writerFields(variant.Fields)})
			}
			literal += fmt.Sprintf(", Type: %q, Variants: []er.CodecVariant{%s}, Construct: func(variant int, fields []any) any {\nswitch variant {\n%s}\npanic(\"unknown variant\")\n}", node.Type, strings.Join(variants, ", "), strings.Join(cases, ""))
		case "transform":
			literal += fmt.Sprintf(", Type: %q, Child: %d, Step: %d", node.Type, node.Child, node.Step)
			writer.Child, writer.Step = node.Child, node.Step
			for len(steps["decode"]) <= node.Step {
				steps["decode"] = append(steps["decode"], "nil")
				steps["encode"] = append(steps["encode"], "nil")
			}
			decode, encode := codecVectorGoBehavior(t, position, plan, node.Child, node.Behavior)
			steps["decode"][node.Step], steps["encode"][node.Step] = decode, encode
		}
		nodes = append(nodes, literal+"}")
		writers = append(writers, writer)
	}
	fmt.Fprintf(&out, "var %s_plan = compile(er.CodecPlan{Profile: %q, Bounds: er.CodecBounds{MaxBodyBytes: %d, MaxDepth: %d}, Root: %d, Nodes: []er.CodecNode{\n%s,\n}})\n", prefix, profile, plan.Bounds.MaxBodyBytes, plan.Bounds.MaxDepth, plan.Root, strings.Join(nodes, ",\n"))
	decode := "[]er.CodecStep(nil)"
	if len(steps["decode"]) > 0 {
		decode = "[]er.CodecStep{\n" + strings.Join(steps["decode"], ",\n") + ",\n}"
	}
	fmt.Fprintf(&out, "var %s_decode = %s\n", prefix, decode)
	// Each encode behavior is a named function the writer calls directly,
	// as a generated writer calls its witness's encode function.
	encode := []goCodecWriterStep{}
	for step, code := range steps["encode"] {
		function := fmt.Sprintf("%s_encode%d", prefix, step)
		fmt.Fprintf(&out, "func %s%s\n", function, strings.TrimPrefix(code, "func"))
		encode = append(encode, goCodecWriterStep{Function: function, Form: goCodecStepFallible})
	}
	out.WriteString(goCodecWriters(prefix+"_plan", strconv.Itoa(position), plan.Root, writers, encode))
	return out.String()
}

// codecVectorGoBehavior implements a target-neutral behavior between its
// string domain and its representation node's generated carrier: an untyped
// decode step for the plan and a typed encode step for the writer.
func codecVectorGoBehavior(t *testing.T, position int, plan codecVectorPlan, child int, behavior string) (string, string) {
	t.Helper()
	representation := codecVectorGoType(position, plan, child)
	variant := func(tag string) string {
		for v, candidate := range plan.Nodes[child].Variants {
			if candidate.Tag == tag {
				return codecVectorGoVariant(position, child, v)
			}
		}
		t.Fatalf("behavior %s needs variant %s", behavior, tag)
		return ""
	}
	switch behavior {
	case "email":
		return `func(value any) (any, error) {
	if value.(string) == "" {
		return nil, &stepFailure{"email", "empty"}
	}
	return strings.ToLower(value.(string)), nil
}`, `func(value string) (string, error) {
	if value == "blocked" {
		return "", &stepFailure{"email", "blocked"}
	}
	return value, nil
}`
	case "status":
		return fmt.Sprintf(`func(value any) (any, error) {
	if _, ok := value.(%s); ok {
		return "Suspended", nil
	}
	return "Active", nil
}`, variant("Suspended")), fmt.Sprintf(`func(value string) (%s, error) {
	if value == "Suspended" {
		return %s{}, nil
	}
	return %s{}, nil
}`, representation, variant("Suspended"), variant("Active"))
	case "box":
		return fmt.Sprintf("func(value any) (any, error) { return value.(%s).F0, nil }", representation),
			fmt.Sprintf("func(value string) (%s, error) { return %s{F0: value}, nil }", representation, representation)
	}
	t.Fatalf("Go harness has no behavior %q", behavior)
	return "", ""
}

// codecVectorGoLiteral renders a neutral encode value as a Go literal of
// the node's generated carrier.
func codecVectorGoLiteral(t *testing.T, position int, plan codecVectorPlan, index int, value any) string {
	t.Helper()
	node := plan.Nodes[index]
	marker, _ := value.(map[string]any)
	fields := func(goType string, list []codecVectorField, source map[string]any) string {
		values := []string{}
		for i, field := range list {
			values = append(values, fmt.Sprintf("F%d: %s", i, codecVectorGoLiteral(t, position, plan, field.Node, source[field.Name])))
		}
		return goType + "{" + strings.Join(values, ", ") + "}"
	}
	switch node.Kind {
	case "string", "transform":
		// A transformed position holds its behavior's string domain.
		if text, ok := value.(string); ok {
			return strconv.Quote(text)
		}
		raw, err := hex.DecodeString(marker["$hex"].(string))
		if err != nil {
			t.Fatal(err)
		}
		return strconv.Quote(string(raw))
	case "bool":
		return strconv.FormatBool(value.(bool))
	case "void":
		return "struct{}{}"
	case "i64":
		number, err := strconv.ParseInt(marker["$i64"].(string), 10, 64)
		if err != nil {
			t.Fatalf("Go cannot carry i64 %v: %v", marker, err)
		}
		return "int64(" + strconv.FormatInt(number, 10) + ")"
	case "record":
		return fields(codecVectorGoName(position, index), node.Fields, marker)
	case "union":
		for v, variant := range node.Variants {
			if variant.Tag == marker["$variant"] {
				source, _ := marker["fields"].(map[string]any)
				return codecVectorGoName(position, index) + "(" + fields(codecVectorGoVariant(position, index, v), variant.Fields, source) + ")"
			}
		}
	}
	t.Fatalf("neutral value %v does not fit node %d", value, index)
	return ""
}
