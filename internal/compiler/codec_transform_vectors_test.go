package compiler

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	rt "effra.local/prototype/runtime/effra"
)

// The transformation vectors extend the structural policy vectors with
// transform nodes. Each node names a target-neutral behavior that the Go
// harness implements and the pinned Effect comparator implemented when its
// frozen output was recorded; every expectation is an Effra outcome.
type codecTransformDocument struct {
	SchemaVersion int    `json:"schemaVersion"`
	Profile       string `json:"profile"`
	Comparator    struct {
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

// runCodecTransformVectorsGo runs the vectors on the generated Go path; a
// decoded value is re-encoded, and a re-encode failure is staged apart.
func runCodecTransformVectorsGo(t *testing.T, document codecTransformDocument) map[string]codecOutcome {
	t.Helper()
	jobs := []codecVectorJob{}
	for _, vector := range document.Vectors {
		if vector.Direction == "decode" {
			jobs = append(jobs, codecVectorJob{id: vector.ID, plan: vector.Plan, body: []byte(*vector.Body), decode: true})
			continue
		}
		var value any
		if err := json.Unmarshal(vector.Value, &value); err != nil {
			t.Fatalf("%s: %v", vector.ID, err)
		}
		jobs = append(jobs, codecVectorJob{id: vector.ID, plan: vector.Plan, value: value})
	}
	results := runCodecVectorsGenerated(t, document.Profile, document.Plans, jobs)
	for _, planError := range document.PlanErrors {
		if _, err := rt.CompileCodec(codecVectorGoPlan(planError.Plan, document.Profile)); err == nil {
			t.Fatalf("%s: Go accepted an invalid plan", planError.ID)
		} else {
			results[planError.ID] = codecOutcome{Reason: err.Error()}
		}
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
// vector on the native Go engine, with the same obligations as the structural
// vectors: Go produces the Effra outcome, and each recorded comparison is
// what the frozen pinned Effect Schema output implies.
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
			t.Fatalf("vector %s must run on Go with a text body (decode) or value (encode)", vector.ID)
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
	for _, vector := range document.Vectors {
		if actual, ok := goResults[vector.ID]; !ok {
			t.Errorf("%s: go did not run", vector.ID)
		} else if actual.String() != vector.Expect.String() {
			t.Errorf("%s: go produced %s, want %s", vector.ID, actual, vector.Expect)
		}
	}
	for _, planError := range document.PlanErrors {
		if message := goResults[planError.ID].Reason; !strings.Contains(message, planError.Expect) {
			t.Errorf("%s: go plan error %q lacks %q", planError.ID, message, planError.Expect)
		}
	}

	retained, err := os.ReadFile(filepath.Join(root, document.Comparator.Output))
	if err != nil {
		t.Fatal(err)
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
