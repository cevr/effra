package compiler

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	rt "effra.local/prototype/runtime/effra"
)

// The transformation vectors extend the structural policy vectors with
// transform nodes. Each node names a target-neutral behavior that the Go
// harness, the JavaScript harness and the pinned Effect comparator implement
// independently; every expectation is an Effra outcome.
type codecTransformDocument struct {
	SchemaVersion int    `json:"schemaVersion"`
	Profile       string `json:"profile"`
	Comparator    struct {
		Script string `json:"script"`
		Output string `json:"output"`
	} `json:"comparator"`
	Behaviors  map[string]string          `json:"behaviors"`
	Plans      map[string]codecVectorPlan `json:"plans"`
	Vectors    []codecVector              `json:"vectors"`
	PlanErrors []struct {
		ID     string          `json:"id"`
		Expect string          `json:"expect"`
		Plan   codecVectorPlan `json:"plan"`
	} `json:"planErrors"`
}

const codecTransformVectorsPath = "conformance/codecs/json-transform-vectors.json"

// codecVectorStepFailure is a behavior's own failure. The engines return it
// unchanged, so it carries no codec path.
type codecVectorStepFailure struct{ step, message string }

func (f *codecVectorStepFailure) Error() string { return f.step + ": " + f.message }

func codecTransformBehaviorsGo() map[string]map[string]rt.CodecStep {
	refuse := func(step, message string) error { return &codecVectorStepFailure{step: step, message: message} }
	return map[string]map[string]rt.CodecStep{
		"email": {
			"decode": func(value any) (any, error) {
				if value.(string) == "" {
					return nil, refuse("email", "empty")
				}
				return strings.ToLower(value.(string)), nil
			},
			"encode": func(value any) (any, error) {
				if value.(string) == "blocked" {
					return nil, refuse("email", "blocked")
				}
				return value, nil
			},
		},
		"status": {
			"decode": func(value any) (any, error) {
				if value.(codecVectorCarrier).variant == 2 {
					return "Suspended", nil
				}
				return "Active", nil
			},
			"encode": func(value any) (any, error) {
				return codecVectorCarrier{variant: map[string]int{"Active": 0, "Suspended": 2}[value.(string)]}, nil
			},
		},
		"box": {
			"decode": func(value any) (any, error) { return value.(codecVectorCarrier).fields[0], nil },
			"encode": func(value any) (any, error) { return codecVectorCarrier{fields: []any{value}}, nil },
		},
	}
}

func codecTransformStepsGo(t *testing.T, plan codecVectorPlan, direction string) []rt.CodecStep {
	t.Helper()
	behaviors := codecTransformBehaviorsGo()
	var steps []rt.CodecStep
	for _, node := range plan.Nodes {
		if node.Kind != "transform" {
			continue
		}
		for len(steps) <= node.Step {
			steps = append(steps, nil)
		}
		steps[node.Step] = behaviors[node.Behavior][direction]
		if steps[node.Step] == nil {
			t.Fatalf("Go harness has no %s behavior %q", direction, node.Behavior)
		}
	}
	return steps
}

func runCodecTransformVectorsGo(t *testing.T, document codecTransformDocument) map[string]codecOutcome {
	t.Helper()
	settle := func(stage string, output []byte, err error) codecOutcome {
		var failure *rt.CodecError
		var step *codecVectorStepFailure
		switch {
		case errors.As(err, &failure):
			return codecOutcome{Reason: stage + string(failure.Reason), Path: failure.Path, Offset: failure.Offset}
		case errors.As(err, &step):
			return codecOutcome{Step: stage + step.step, Message: step.message}
		case err != nil:
			t.Fatalf("untyped codec failure: %v", err)
		}
		return codecOutcome{OK: true, Encoded: string(output)}
	}
	results := map[string]codecOutcome{}
	for _, vector := range document.Vectors {
		plan := document.Plans[vector.Plan]
		codec, err := rt.CompileCodec(codecVectorGoPlan(plan, document.Profile))
		if err != nil {
			t.Fatalf("plan %s: %v", vector.Plan, err)
		}
		decodeSteps, encodeSteps := codecTransformStepsGo(t, plan, "decode"), codecTransformStepsGo(t, plan, "encode")
		if vector.Direction == "decode" {
			value, err := codec.DecodeSteps([]byte(*vector.Body), decodeSteps)
			if err != nil {
				results[vector.ID] = settle("", nil, err)
				continue
			}
			output, err := codec.EncodeSteps(value, encodeSteps)
			results[vector.ID] = settle("re-encode ", output, err)
			continue
		}
		var value any
		if err := json.Unmarshal(vector.Value, &value); err != nil {
			t.Fatalf("%s: %v", vector.ID, err)
		}
		output, err := codec.EncodeSteps(codecVectorGoValue(t, plan, plan.Root, value), encodeSteps)
		results[vector.ID] = settle("", output, err)
	}
	for _, planError := range document.PlanErrors {
		if _, err := rt.CompileCodec(codecVectorGoPlan(planError.Plan, document.Profile)); err == nil {
			t.Fatalf("%s: Go accepted an invalid plan", planError.ID)
		} else {
			results[planError.ID] = codecOutcome{Reason: err.Error()}
		}
	}
	return results
}

// codecTransformHarness drives the embedded JavaScript engine's resolve and
// prepare generators synchronously with the same behaviors.
const codecTransformHarness = `
const document = JSON.parse(readFileSync(process.argv[2], "utf8"));
class StepFailure { constructor(step, message) { this.step = step; this.message = message; } }
const behaviors = {
  email: {
    decode: text => { if (text === "") throw new StepFailure("email", "empty"); return text.toLowerCase(); },
    encode: text => { if (text === "blocked") throw new StepFailure("email", "blocked"); return text; },
  },
  status: {
    decode: wire => wire._tag === "StatusWire.Suspended" ? "Suspended" : "Active",
    encode: status => ({ _tag: "StatusWire." + status }),
  },
  box: { decode: box => box.value, encode: value => ({ value }) },
};
const stepsFor = (plan, direction) => {
  const steps = [];
  for (const node of plan.nodes) if (node.kind === "transform") steps[node.step] = behaviors[node.behavior][direction];
  return steps;
};
const drive = (requests, steps) => {
  let next = requests.next();
  while (!next.done) next = requests.next(steps[next.value.step](next.value.value));
  return next.value;
};
const domain = (plan, index, value) => {
  const node = plan.nodes[index];
  if (node.kind === "record") return Object.fromEntries(node.fields.map(field => [field.name, domain(plan, field.node, value[field.name])]));
  return value;
};
const issue = (id, stage, result) => ({ id, ok: false, reason: stage + result.issue.reason, path: result.issue.path, offset: result.issue.offset });
const stepped = (id, stage, run) => {
  try { return run(); } catch (error) {
    if (!(error instanceof StepFailure)) throw error;
    return { id, ok: false, step: stage + error.step, message: error.message };
  }
};
const encode = (id, stage, codec, plan, value) => stepped(id, stage, () => {
  const result = codec.pending ? drive(codec.stream(value), stepsFor(plan, "encode")) : codec.encode(value);
  return result.ok ? { id, ok: true, encoded: new TextDecoder("utf-8", { fatal: true }).decode(result.bytes) } : issue(id, stage, result);
});
const lines = [];
for (const vector of document.vectors) {
  const plan = document.plans[vector.plan];
  const codec = __ef_codecCompile({ profile: document.profile, ...plan });
  if (codec.steps !== stepsFor(plan, "decode").length) throw new Error(vector.id + ": step count");
  if (vector.direction === "decode") {
    const decoded = codec.decode(new TextEncoder().encode(vector.body));
    if (!decoded.ok) { lines.push(issue(vector.id, "", decoded)); continue; }
    const resolved = stepped(vector.id, "", () => ({ value: codec.pending ? drive(codec.resolve(decoded.value), stepsFor(plan, "decode")) : decoded.value }));
    lines.push(resolved.step === undefined ? encode(vector.id, "re-encode ", codec, plan, resolved.value) : resolved);
  } else {
    lines.push(encode(vector.id, "", codec, plan, domain(plan, plan.root, vector.value)));
  }
}
for (const planError of document.planErrors) {
  try {
    __ef_codecCompile({ profile: document.profile, ...planError.plan });
    throw new Error(planError.id + ": JS accepted an invalid plan");
  } catch (error) {
    if (!error.message.startsWith("invalid codec plan: ")) throw error;
    lines.push({ id: planError.id, ok: false, reason: error.message });
  }
}
for (const line of lines) console.log(JSON.stringify(line));
`

func runCodecTransformVectorsJS(t *testing.T, bun, root string) map[string]codecOutcome {
	t.Helper()
	vectors, err := filepath.Abs(filepath.Join(root, codecTransformVectorsPath))
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]codecOutcome{}
	for _, line := range strings.Split(strings.TrimSpace(string(runCodecJS(t, bun, root, codecTransformHarness, vectors))), "\n") {
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

// codecTransformComparison classifies an Effra outcome against the
// comparator line. A step failure matches a Schema failure with the same
// message; Effra returns the step's failure unchanged and so reports no
// codec path for it. Structural outcomes use the structural rules.
func codecTransformComparison(expect codecOutcome, effect codecEffectLine) string {
	if expect.Step != "" {
		if !effect.OK && effect.Message == expect.Message {
			return "matched"
		}
		return "difference"
	}
	return codecComparison(expect, effect, "")
}

// TestCodecTransformVectorsAcrossGoJSAndEffect executes every transformation
// vector on the native Go engine, the embedded JavaScript engine and pinned
// Effect Schema, with the same obligations as the structural vectors: Go and
// JS produce the Effra outcome, the live comparator reproduces the retained
// bytes, and each recorded comparison is what that output implies.
func TestCodecTransformVectorsAcrossGoJSAndEffect(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, codecTransformVectorsPath))
	if err != nil {
		t.Fatal(err)
	}
	var document codecTransformDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("transformation vectors: %v", err)
	}
	if document.SchemaVersion != 1 || document.Profile != rt.CodecProfileJSON {
		t.Fatalf("transformation vectors target schema %d profile %q", document.SchemaVersion, document.Profile)
	}
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Fatal("Bun is required for codec conformance vectors")
	}
	used := map[string]bool{}
	for name, plan := range document.Plans {
		for _, node := range plan.Nodes {
			if node.Kind == "transform" {
				if _, ok := document.Behaviors[node.Behavior]; !ok {
					t.Fatalf("plan %s names undocumented behavior %q", name, node.Behavior)
				}
				used[node.Behavior] = true
			}
		}
	}
	for behavior := range document.Behaviors {
		if !used[behavior] {
			t.Fatalf("behavior %s is documented but unused", behavior)
		}
	}
	seen := map[string]bool{}
	outcomes := map[string]bool{}
	for _, vector := range document.Vectors {
		if seen[vector.ID] {
			t.Fatalf("vector %s is repeated", vector.ID)
		}
		seen[vector.ID] = true
		if _, ok := document.Plans[vector.Plan]; !ok {
			t.Fatalf("vector %s names unknown plan %q", vector.ID, vector.Plan)
		}
		if vector.Targets != nil || vector.BodyHex != "" || (vector.Direction == "decode") != (vector.Body != nil) || (vector.Direction == "encode") != (vector.Value != nil) {
			t.Fatalf("vector %s must run on both targets with a text body (decode) or value (encode)", vector.ID)
		}
		switch vector.Comparison {
		case "matched":
		case "difference":
			if vector.Note == "" {
				t.Fatalf("vector %s records a difference without a note", vector.ID)
			}
		default:
			t.Fatalf("vector %s has comparison %q", vector.ID, vector.Comparison)
		}
		kind := "structural"
		if vector.Expect.OK {
			kind = "ok"
		} else if vector.Expect.Step != "" {
			kind = "step"
		}
		outcomes[vector.Direction+" "+kind] = true
	}
	for _, required := range []string{"decode ok", "decode step", "decode structural", "encode ok", "encode step", "encode structural"} {
		if !outcomes[required] {
			t.Fatalf("transformation vectors cover no %s outcome", required)
		}
	}

	goResults := runCodecTransformVectorsGo(t, document)
	jsResults := runCodecTransformVectorsJS(t, bun, root)
	for target, results := range map[string]map[string]codecOutcome{"go": goResults, "js": jsResults} {
		for _, vector := range document.Vectors {
			if actual, ok := results[vector.ID]; !ok {
				t.Errorf("%s: %s did not run", vector.ID, target)
			} else if actual.String() != vector.Expect.String() {
				t.Errorf("%s: %s produced %s, want %s", vector.ID, target, actual, vector.Expect)
			}
		}
		for _, planError := range document.PlanErrors {
			if message := results[planError.ID].Reason; !strings.Contains(message, planError.Expect) {
				t.Errorf("%s: %s plan error %q lacks %q", planError.ID, target, message, planError.Expect)
			}
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
	differences := []string{}
	for i, vector := range document.Vectors {
		var effect codecEffectLine
		if err := json.Unmarshal([]byte(lines[i]), &effect); err != nil || effect.ID != vector.ID {
			t.Fatalf("comparator line %d is not vector %s: %v %s", i+1, vector.ID, err, lines[i])
		}
		if comparison := codecTransformComparison(vector.Expect, effect); comparison != vector.Comparison {
			t.Errorf("%s: recorded %s but pinned Effect output %s implies %s", vector.ID, vector.Comparison, lines[i], comparison)
		}
		if vector.Comparison == "difference" {
			differences = append(differences, vector.ID)
		}
	}
	// The structure-first ordering is the deliberate difference from Schema's
	// per-field transformations; it must stay executed evidence.
	if !slices.Contains(differences, "decode.structure-before-transformation") {
		t.Fatal("structure-first decoding is no longer a recorded difference from pinned Schema")
	}
}
