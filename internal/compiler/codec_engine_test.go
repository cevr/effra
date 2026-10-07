package compiler

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	rt "effra.local/prototype/runtime/effra"
)

// The policy vectors are target-neutral: plans name nodes by index, encode
// values use $-markers for carriers JSON cannot spell, and every expectation
// is an Effra outcome. The comparator file is the executed pinned Effect
// Schema behavior for the same vectors.
type codecVectorDocument struct {
	SchemaVersion int    `json:"schemaVersion"`
	Profile       string `json:"profile"`
	Comparator    struct {
		Script       string `json:"script"`
		Output       string `json:"output"`
		ParseFailure string `json:"parseFailure"`
	} `json:"comparator"`
	Plans      map[string]codecVectorPlan `json:"plans"`
	Vectors    []codecVector              `json:"vectors"`
	PlanErrors []struct {
		ID      string          `json:"id"`
		Expect  string          `json:"expect"`
		Profile string          `json:"profile"`
		Plan    codecVectorPlan `json:"plan"`
	} `json:"planErrors"`
}

type codecVectorPlan struct {
	Bounds struct {
		MaxBodyBytes int `json:"maxBodyBytes"`
		MaxDepth     int `json:"maxDepth"`
	} `json:"bounds"`
	Root  int `json:"root"`
	Nodes []struct {
		Kind     string             `json:"kind"`
		Type     string             `json:"type"`
		Fields   []codecVectorField `json:"fields"`
		Variants []struct {
			Tag       string             `json:"tag"`
			DomainTag string             `json:"domainTag"`
			Fields    []codecVectorField `json:"fields"`
		} `json:"variants"`
	} `json:"nodes"`
}

type codecVectorField struct {
	Name string `json:"name"`
	Node int    `json:"node"`
}

type codecVector struct {
	ID         string          `json:"id"`
	Plan       string          `json:"plan"`
	Direction  string          `json:"direction"`
	Body       *string         `json:"body"`
	BodyHex    string          `json:"bodyHex"`
	Value      json.RawMessage `json:"value"`
	Targets    []string        `json:"targets"`
	Expect     codecOutcome    `json:"expect"`
	Comparison string          `json:"comparison"`
	Note       string          `json:"note"`
}

// codecOutcome is one Effra engine result: encoded output on success (for
// decode vectors, the decoded value re-encoded), or the typed failure.
type codecOutcome struct {
	OK      bool     `json:"ok"`
	Encoded string   `json:"encoded"`
	Reason  string   `json:"reason"`
	Path    []string `json:"path"`
	Offset  int      `json:"offset"`
}

func (o codecOutcome) String() string {
	if o.OK {
		return "ok " + strconv.Quote(o.Encoded)
	}
	return fmt.Sprintf("%s at byte %d path %q", o.Reason, o.Offset, o.Path)
}

// codecEffectLine is one line of the retained comparator output.
type codecEffectLine struct {
	ID      string   `json:"id"`
	OK      bool     `json:"ok"`
	Encoded string   `json:"encoded"`
	Path    []string `json:"path"`
	Message string   `json:"message"`
	Skipped string   `json:"skipped"`
}

func readCodecVectors(t *testing.T) (codecVectorDocument, string) {
	t.Helper()
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "conformance", "codecs", "json-structural-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document codecVectorDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("policy vectors: %v", err)
	}
	if document.SchemaVersion != 1 || document.Profile != rt.CodecProfileJSON {
		t.Fatalf("policy vectors target schema %d profile %q", document.SchemaVersion, document.Profile)
	}
	return document, root
}

// TestCodecPolicyVectorsAcrossGoJSAndEffect executes every policy vector on
// the native Go engine, the embedded JavaScript engine and pinned Effect
// Schema. Go and JS must both produce the vector's Effra outcome; the live
// comparator must reproduce the retained bytes exactly; and each vector's
// recorded comparison must be what the executed comparator output implies.
func TestCodecPolicyVectorsAcrossGoJSAndEffect(t *testing.T) {
	document, root := readCodecVectors(t)
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("Bun is required for codec conformance vectors")
	}
	seen := map[string]bool{}
	for _, vector := range document.Vectors {
		if seen[vector.ID] {
			t.Fatalf("vector %s is repeated", vector.ID)
		}
		seen[vector.ID] = true
		if _, ok := document.Plans[vector.Plan]; !ok {
			t.Fatalf("vector %s names unknown plan %q", vector.ID, vector.Plan)
		}
		switch vector.Direction {
		case "decode":
			if vector.Targets != nil || vector.Value != nil {
				t.Fatalf("decode vector %s runs on both targets and has a body only", vector.ID)
			}
		case "encode":
			if len(vector.Targets) == 0 || slices.ContainsFunc(vector.Targets, func(target string) bool { return target != "go" && target != "js" }) {
				t.Fatalf("encode vector %s has targets %q", vector.ID, vector.Targets)
			}
		default:
			t.Fatalf("vector %s has direction %q", vector.ID, vector.Direction)
		}
		// The comparator skips not-applicable vectors, so the label is only
		// admitted where Effect cannot hold the value: a value no JS carrier
		// represents.
		if vector.Comparison == "not-applicable" && (vector.Direction != "encode" || slices.Contains(vector.Targets, "js")) {
			t.Fatalf("vector %s is not-applicable although JS carries it", vector.ID)
		}
		switch vector.Comparison {
		case "matched":
		case "difference", "not-applicable":
			if vector.Note == "" {
				t.Fatalf("vector %s records a %s without a note", vector.ID, vector.Comparison)
			}
		default:
			t.Fatalf("vector %s has comparison %q", vector.ID, vector.Comparison)
		}
	}
	for _, policy := range []string{"required", "excess", "null", "duplicate", "utf8", "union", "string", "i64", "depth", "body", "void", "failure", "encode"} {
		covered := false
		for id := range seen {
			covered = covered || strings.HasPrefix(id, policy+".")
		}
		if !covered {
			t.Fatalf("policy vectors cover no %s cases", policy)
		}
	}

	goResults := runCodecVectorsGo(t, document)
	jsResults := runCodecVectorsJS(t, bun, root)
	for _, vector := range document.Vectors {
		for _, target := range codecVectorTargets(vector) {
			results := map[string]map[string]codecOutcome{"go": goResults, "js": jsResults}[target]
			actual, ok := results[vector.ID]
			if !ok {
				t.Errorf("%s: %s did not run", vector.ID, target)
				continue
			}
			if actual.String() != vector.Expect.String() {
				t.Errorf("%s: %s produced %s, want %s", vector.ID, target, actual, vector.Expect)
			}
		}
		for target, results := range map[string]map[string]codecOutcome{"go": goResults, "js": jsResults} {
			if _, ran := results[vector.ID]; ran && !slices.Contains(codecVectorTargets(vector), target) {
				t.Errorf("%s: %s ran a vector it cannot represent", vector.ID, target)
			}
		}
	}
	for _, planError := range document.PlanErrors {
		if message := goResults[planError.ID].Reason; !strings.Contains(message, planError.Expect) {
			t.Errorf("%s: Go plan error %q lacks %q", planError.ID, message, planError.Expect)
		}
		if message := jsResults[planError.ID].Reason; !strings.Contains(message, planError.Expect) {
			t.Errorf("%s: JS plan error %q lacks %q", planError.ID, message, planError.Expect)
		}
	}

	command := exec.Command(bun, document.Comparator.Script)
	command.Dir = root
	command.Stderr = os.Stderr
	live, err := command.Output()
	if err != nil {
		t.Fatalf("pinned Effect comparator: %v", err)
	}
	retained, err := os.ReadFile(filepath.Join(root, document.Comparator.Output))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(live, retained) {
		t.Fatalf("pinned Effect comparator output differs from %s; regenerate with `bun %s > %s` and review every changed line", document.Comparator.Output, document.Comparator.Script, document.Comparator.Output)
	}
	lines := strings.Split(strings.TrimSuffix(string(retained), "\n"), "\n")
	if len(lines) != len(document.Vectors) {
		t.Fatalf("comparator has %d lines for %d vectors", len(lines), len(document.Vectors))
	}
	for i, vector := range document.Vectors {
		var effect codecEffectLine
		if err := json.Unmarshal([]byte(lines[i]), &effect); err != nil || effect.ID != vector.ID {
			t.Fatalf("comparator line %d is not vector %s: %v %s", i+1, vector.ID, err, lines[i])
		}
		if comparison := codecComparison(vector.Expect, effect, document.Comparator.ParseFailure); comparison != vector.Comparison {
			t.Errorf("%s: recorded %s but pinned Effect output %s implies %s", vector.ID, vector.Comparison, lines[i], comparison)
		}
	}
}

func codecVectorTargets(vector codecVector) []string {
	if vector.Direction == "decode" {
		return []string{"go", "js"}
	}
	return vector.Targets
}

// codecComparison classifies an Effra outcome against the comparator line.
// Success matches identical output. A failure matches only at the same path
// and with the corresponding Schema issue: an Effra syntax failure is a
// JSON.parse rejection; each structural reason has its Schema message family.
// Bound, duplicate-key and Unicode admission have no Schema counterpart.
func codecComparison(expect codecOutcome, effect codecEffectLine, parseFailure string) string {
	if effect.Skipped != "" {
		return "not-applicable"
	}
	if expect.OK != effect.OK {
		return "difference"
	}
	if expect.OK {
		if expect.Encoded == effect.Encoded {
			return "matched"
		}
		return "difference"
	}
	if !slices.Equal(expect.Path, effect.Path) {
		return "difference"
	}
	message := effect.Message
	integer := message == "Expected a string representing a bigint"
	bounded := strings.HasPrefix(message, "Expected a value between ")
	matched := false
	switch rt.CodecReason(expect.Reason) {
	case rt.CodecSyntax:
		matched = message == parseFailure
	case rt.CodecMissing:
		matched = message == "Missing key"
	case rt.CodecInteger:
		matched = integer
	case rt.CodecRange:
		matched = bounded
	case rt.CodecType, rt.CodecTag:
		matched = strings.HasPrefix(message, "Expected ") && message != parseFailure && !integer && !bounded
	}
	if matched {
		return "matched"
	}
	return "difference"
}

// codecVectorCarrier stands in for compiler-generated record and union
// carriers: the variant index and declared-order field values.
type codecVectorCarrier struct {
	variant int
	fields  []any
}

func codecVectorGoPlan(plan codecVectorPlan, profile string) rt.CodecPlan {
	construct := func(variant int, fields []any) any { return codecVectorCarrier{variant: variant, fields: fields} }
	project := func(value any) (int, []any) {
		carrier := value.(codecVectorCarrier)
		return carrier.variant, carrier.fields
	}
	fields := func(list []codecVectorField) []rt.CodecField {
		out := []rt.CodecField{}
		for _, field := range list {
			out = append(out, rt.CodecField{Name: field.Name, Node: field.Node})
		}
		return out
	}
	out := rt.CodecPlan{Profile: profile, Bounds: rt.CodecBounds{MaxBodyBytes: plan.Bounds.MaxBodyBytes, MaxDepth: plan.Bounds.MaxDepth}, Root: plan.Root}
	for _, node := range plan.Nodes {
		compiled := rt.CodecNode{Kind: rt.CodecKind(node.Kind), Type: node.Type}
		switch compiled.Kind {
		case rt.CodecRecord:
			compiled.Fields = fields(node.Fields)
			compiled.Construct, compiled.Project = construct, project
		case rt.CodecUnion:
			compiled.Variants = []rt.CodecVariant{}
			for _, variant := range node.Variants {
				compiled.Variants = append(compiled.Variants, rt.CodecVariant{Tag: variant.Tag, Fields: fields(variant.Fields)})
			}
			compiled.Construct, compiled.Project = construct, project
		}
		out.Nodes = append(out.Nodes, compiled)
	}
	return out
}

// codecVectorGoValue builds a Go carrier from a neutral encode value.
func codecVectorGoValue(t *testing.T, plan codecVectorPlan, index int, value any) any {
	node := plan.Nodes[index]
	marker, _ := value.(map[string]any)
	fields := func(list []codecVectorField, source map[string]any) []any {
		out := []any{}
		for _, field := range list {
			out = append(out, codecVectorGoValue(t, plan, field.Node, source[field.Name]))
		}
		return out
	}
	switch node.Kind {
	case "string":
		if text, ok := value.(string); ok {
			return text
		}
		raw, err := hex.DecodeString(marker["$hex"].(string))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	case "bool":
		return value.(bool)
	case "void":
		return struct{}{}
	case "i64":
		number, err := strconv.ParseInt(marker["$i64"].(string), 10, 64)
		if err != nil {
			t.Fatalf("Go cannot carry i64 %v: %v", marker, err)
		}
		return number
	case "record":
		return codecVectorCarrier{fields: fields(node.Fields, marker)}
	case "union":
		for position, variant := range node.Variants {
			if variant.Tag == marker["$variant"] {
				return codecVectorCarrier{variant: position, fields: fields(variant.Fields, marker["fields"].(map[string]any))}
			}
		}
	}
	t.Fatalf("neutral value %v does not fit node %d", value, index)
	return nil
}

// runCodecVectorsGo returns outcomes by vector id, and plan-error messages
// (as Reason) by plan-error id.
func runCodecVectorsGo(t *testing.T, document codecVectorDocument) map[string]codecOutcome {
	t.Helper()
	settle := func(output []byte, err error) codecOutcome {
		var failure *rt.CodecError
		if errors.As(err, &failure) {
			return codecOutcome{Reason: string(failure.Reason), Path: failure.Path, Offset: failure.Offset}
		}
		if err != nil {
			t.Fatalf("untyped codec failure: %v", err)
		}
		return codecOutcome{OK: true, Encoded: string(output)}
	}
	codecs := map[string]*rt.Codec{}
	for name, plan := range document.Plans {
		codec, err := rt.CompileCodec(codecVectorGoPlan(plan, document.Profile))
		if err != nil {
			t.Fatalf("plan %s: %v", name, err)
		}
		codecs[name] = codec
	}
	results := map[string]codecOutcome{}
	for _, vector := range document.Vectors {
		codec := codecs[vector.Plan]
		switch {
		case vector.Direction == "decode":
			body := []byte{}
			if vector.Body != nil {
				body = []byte(*vector.Body)
			} else if decoded, err := hex.DecodeString(vector.BodyHex); err == nil {
				body = decoded
			} else {
				t.Fatalf("%s: %v", vector.ID, err)
			}
			value, err := codec.Decode(body)
			if err != nil {
				results[vector.ID] = settle(nil, err)
				continue
			}
			// A decoded value must re-encode; a failure here is reported
			// apart from decode failures so encode checks cannot mask them.
			outcome := settle(codec.Encode(value))
			if !outcome.OK {
				outcome.Reason = "re-encode " + outcome.Reason
			}
			results[vector.ID] = outcome
		case slices.Contains(vector.Targets, "go"):
			var value any
			if err := json.Unmarshal(vector.Value, &value); err != nil {
				t.Fatalf("%s: %v", vector.ID, err)
			}
			plan := document.Plans[vector.Plan]
			results[vector.ID] = settle(codec.Encode(codecVectorGoValue(t, plan, plan.Root, value)))
		}
	}
	for _, planError := range document.PlanErrors {
		profile := document.Profile
		if planError.Profile != "" {
			profile = planError.Profile
		}
		_, err := rt.CompileCodec(codecVectorGoPlan(planError.Plan, profile))
		if err == nil {
			t.Fatalf("%s: Go accepted an invalid plan", planError.ID)
		}
		results[planError.ID] = codecOutcome{Reason: err.Error()}
	}
	return results
}

// codecVectorHarness drives the embedded JavaScript engine over the same
// document and prints one outcome line per executed vector and plan error.
const codecVectorHarness = `
const document = JSON.parse(readFileSync(process.argv[2], "utf8"));
const codecs = new Map();
const codecFor = name => {
  if (!codecs.has(name)) codecs.set(name, __ef_codecCompile({ profile: document.profile, ...document.plans[name] }));
  return codecs.get(name);
};
const domain = (plan, index, value) => {
  const node = plan.nodes[index];
  const fields = (list, source) => list.map(field => [field.name, domain(plan, field.node, source[field.name])]);
  switch (node.kind) {
    case "string": return typeof value === "string" ? value : String.fromCharCode(...value.$utf16);
    case "bool": return value;
    case "void": return undefined;
    case "i64": return BigInt(value.$i64);
    case "record": return Object.fromEntries(fields(node.fields, value));
    case "union": {
      const variant = node.variants.find(candidate => candidate.tag === value.$variant);
      return Object.fromEntries([["_tag", variant.domainTag], ...fields(variant.fields, value.fields)]);
    }
  }
  throw new Error("neutral value does not fit node " + index);
};
const outcome = (id, result, stage = "") => result.ok
  ? { id, ok: true, encoded: new TextDecoder("utf-8", { fatal: true }).decode(result.bytes) }
  : { id, ok: false, reason: stage + result.issue.reason, path: result.issue.path, offset: result.issue.offset };
const lines = [];
for (const vector of document.vectors) {
  const codec = codecFor(vector.plan);
  if (vector.direction === "decode") {
    const body = vector.bodyHex === undefined ? new TextEncoder().encode(vector.body) : Uint8Array.from(Buffer.from(vector.bodyHex, "hex"));
    const decoded = codec.decode(body);
    lines.push(decoded.ok ? outcome(vector.id, codec.encode(decoded.value), "re-encode ") : outcome(vector.id, decoded));
  } else if (vector.targets.includes("js")) {
    const plan = document.plans[vector.plan];
    lines.push(outcome(vector.id, codec.encode(domain(plan, plan.root, vector.value))));
  }
}
for (const planError of document.planErrors) {
  try {
    __ef_codecCompile({ profile: planError.profile ?? document.profile, ...planError.plan });
    throw new Error(planError.id + ": JS accepted an invalid plan");
  } catch (error) {
    if (!error.message.startsWith("invalid codec plan: ")) throw error;
    lines.push({ id: planError.id, ok: false, reason: error.message });
  }
}
for (const line of lines) console.log(JSON.stringify(line));
`

// runCodecJS executes the embedded engine followed by script; extra
// arguments follow the module path.
func runCodecJS(t *testing.T, bun, root, script string, args ...string) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "dist"), 0755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(filepath.Join(root, "dist"), "codec-engine-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "engine.mjs")
	module := "import { readFileSync } from \"node:fs\";\n" + codecEngineJS + "\n" + script
	if err := os.WriteFile(path, []byte(module), 0644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(bun, append([]string{path}, args...)...)
	command.Stderr = os.Stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("JS codec engine: %v", err)
	}
	return output
}

func runCodecVectorsJS(t *testing.T, bun, root string) map[string]codecOutcome {
	t.Helper()
	vectors, err := filepath.Abs(filepath.Join(root, "conformance", "codecs", "json-structural-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]codecOutcome{}
	for _, line := range strings.Split(strings.TrimSpace(string(runCodecJS(t, bun, root, codecVectorHarness, vectors))), "\n") {
		var result struct {
			ID string `json:"id"`
			codecOutcome
		}
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			t.Fatalf("JS outcome %q: %v", line, err)
		}
		results[result.ID] = result.codecOutcome
	}
	return results
}

// TestCodecJSEngineUsesEffraCarriers checks the JavaScript carrier contract
// that generated modules rely on: plain records with own properties, domain
// union tags, bigint i64 and undefined void, with carrier mismatches thrown
// as defects rather than returned as codec failures.
func TestCodecJSEngineUsesEffraCarriers(t *testing.T) {
	_, root := readCodecVectors(t)
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("Bun is required for codec conformance vectors")
	}
	vectors, err := filepath.Abs(filepath.Join(root, "conformance", "codecs", "json-structural-vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	output := runCodecJS(t, bun, root, `
const check = (condition, message) => { if (!condition) throw new Error(message); };
const defect = (run, message) => {
  try { run(); } catch (error) { check(error.message.startsWith("effra codec: "), message + ": " + error.message); return; }
  throw new Error(message + ": no defect");
};
const account = __ef_codecCompile({ profile: "effra/json-structural-1", ...JSON.parse(readFileSync(process.argv[2], "utf8")).plans.account });
const body = '{"id":"-0042","owner":{"name":"Ada","email":"a"},"state":{"_tag":"Running","runId":"r","owner":{"name":"n","email":"e"}},"active":true,"note":null}';
const decoded = account.decode(new TextEncoder().encode(body));
check(decoded.ok, "decode failed");
const value = decoded.value;
check(Object.getPrototypeOf(value) === Object.prototype && Object.keys(value).join() === "id,owner,state,active,note", "record shape");
check(value.id === -42n && value.active === true && Object.hasOwn(value, "note") && value.note === undefined, "primitive carriers");
check(value.state._tag === "State.Running" && value.state.owner.name === "n", "union carrier uses the domain tag");
check(new TextDecoder().decode(account.encode(value).bytes) === body.replace("-0042", "-42"), "round trip");
defect(() => account.encode({ ...value, state: { ...value.state, _tag: "Running" } }), "wire tag in a carrier");
defect(() => account.encode({ ...value, id: -42 }), "number in an i64 carrier");
defect(() => account.encode({ ...value, note: null }), "null in a void carrier");
defect(() => { const { active, ...rest } = value; account.encode(rest); }, "missing carrier field");
const inherited = Object.create({ name: "x" });
const label = __ef_codecCompile({ profile: "effra/json-structural-1", bounds: { maxBodyBytes: 64, maxDepth: 1 }, root: 1, nodes: [{ kind: "string" }, { kind: "record", type: "Proto", fields: [{ name: "__proto__", node: 0 }] }] });
defect(() => label.encode(inherited), "inherited carrier field");
const proto = label.decode(new TextEncoder().encode('{"__proto__":"x"}'));
check(proto.ok && Object.hasOwn(proto.value, "__proto__") && Object.getPrototypeOf(proto.value) === Object.prototype && ({}).x === undefined, "__proto__ is an own field");
check(new TextDecoder().decode(label.encode(proto.value).bytes) === '{"__proto__":"x"}', "__proto__ round trip");
try { account.decode('{"id":"1"}'); throw new Error("string body accepted"); } catch (error) { check(error instanceof TypeError, "string body"); }
try {
  __ef_codecCompile({ profile: "effra/json-structural-1", bounds: { maxBodyBytes: 64, maxDepth: 1 }, root: 0, nodes: [{ kind: "union", type: "U", variants: [{ tag: "A", domainTag: "U.A", fields: [] }, { tag: "B", domainTag: "U.A", fields: [] }] }] });
  throw new Error("shared domain tag accepted");
} catch (error) { check(error.message.includes("distinct domain tag"), error.message); }
console.log("carriers: passed");
`, vectors)
	if strings.TrimSpace(string(output)) != "carriers: passed" {
		t.Fatalf("JS carrier contract: %s", output)
	}
}

// TestCodecJSEncodeConstructsNoOutputBeyondTheBound encodes megabyte strings,
// including ones whose escaped form is six times longer, under small bounds.
// It observes every string the host string, array and JSON builtins produce
// or receive during encode: none may be longer than MaxBodyBytes, so the
// refusal comes from the allowance check while escaping, not after building
// the whole representation.
func TestCodecJSEncodeConstructsNoOutputBeyondTheBound(t *testing.T) {
	_, root := readCodecVectors(t)
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("Bun is required for codec conformance vectors")
	}
	output := runCodecJS(t, bun, root, `
const inputs = { control: "\u0001".repeat(1 << 20), quote: "\"".repeat(1 << 20), plain: "a".repeat(1 << 20), scalar: "é".repeat(1 << 19) };
let largest = 0;
const observe = value => { if (typeof value === "string" && value.length > largest) largest = value.length; return value; };
const builtins = [[JSON, "stringify"], [String.prototype, "slice"], [String.prototype, "substring"], [String.prototype, "concat"], [String.prototype, "padStart"], [String.prototype, "replace"], [String.prototype, "replaceAll"], [Array.prototype, "push"], [Array.prototype, "join"], [TextEncoder.prototype, "encode"]];
for (const maxBodyBytes of [32, 4096]) {
  const codec = __ef_codecCompile({ profile: "effra/json-structural-1", bounds: { maxBodyBytes, maxDepth: 1 }, root: 0, nodes: [{ kind: "string" }] });
  for (const [name, input] of Object.entries(inputs)) {
    largest = 0;
    const originals = builtins.map(([owner, method]) => owner[method]);
    builtins.forEach(([owner, method], i) => { owner[method] = function (...args) { args.forEach(observe); return observe(originals[i].apply(this, args)); }; });
    let result;
    try { result = codec.encode(input); } finally { builtins.forEach(([owner, method], i) => { owner[method] = originals[i]; }); }
    if (result.ok || result.issue.reason !== "body-too-large") throw new Error(name + " under " + maxBodyBytes + " bytes: " + JSON.stringify(result.issue));
    if (largest > maxBodyBytes) console.log(name + " under " + maxBodyBytes + " bytes built a " + largest + "-unit string");
  }
}
console.log("bounded: done");
`)
	if strings.TrimSpace(string(output)) != "bounded: done" {
		t.Fatalf("JS encode constructed output beyond the bound:\n%s", output)
	}
}
