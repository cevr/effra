package compiler

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	rt "effra.local/prototype/runtime/effra"
)

// The policy vectors are target-neutral: plans name nodes by index, encode
// values use $-markers for carriers JSON cannot spell, and every expectation
// is an Effra outcome. The comparator file is a frozen golden: the pinned
// Effect Schema behavior for the same vectors, recorded when Effect was the
// JavaScript runtime. Encode vector targets name the carriers that can hold
// the value: Go strings, or the JavaScript carriers that comparator used.
type codecVectorDocument struct {
	SchemaVersion int    `json:"schemaVersion"`
	Profile       string `json:"profile"`
	Comparator    struct {
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
		// Transform nodes name their representation node, step index and
		// the target-neutral behavior each harness implements.
		Child    int    `json:"child"`
		Step     int    `json:"step"`
		Behavior string `json:"behavior"`
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
// decode vectors, the decoded value re-encoded), the typed codec failure, or
// a transformation step's own failure, returned unchanged.
type codecOutcome struct {
	OK      bool     `json:"ok"`
	Encoded string   `json:"encoded"`
	Reason  string   `json:"reason"`
	Path    []string `json:"path"`
	Offset  int      `json:"offset"`
	Step    string   `json:"step"`
	Message string   `json:"message"`
}

func (o codecOutcome) String() string {
	if o.OK {
		return "ok " + strconv.Quote(o.Encoded)
	}
	if o.Step != "" {
		return fmt.Sprintf("step %s failed %q", o.Step, o.Message)
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

// TestCodecPolicyVectorsAcrossGoJSAndEffect executes every policy vector a
// Go string can carry on the native Go engine, which must produce the
// vector's Effra outcome, and checks each vector's recorded comparison
// against the frozen pinned Effect Schema output. Vectors only JavaScript
// carriers can hold keep their recorded comparison and are not executed.
func TestCodecPolicyVectorsAcrossGoJSAndEffect(t *testing.T) {
	document, root := readCodecVectors(t)
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
				t.Fatalf("decode vector %s runs on every carrier and has a body only", vector.ID)
			}
		case "encode":
			if len(vector.Targets) == 0 || slices.ContainsFunc(vector.Targets, func(target string) bool { return target != "go" && target != "js" }) {
				t.Fatalf("encode vector %s has targets %q", vector.ID, vector.Targets)
			}
		default:
			t.Fatalf("vector %s has direction %q", vector.ID, vector.Direction)
		}
		// The comparator skipped not-applicable vectors, so the label is only
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
	for _, vector := range document.Vectors {
		carried := vector.Direction == "decode" || slices.Contains(vector.Targets, "go")
		actual, ran := goResults[vector.ID]
		switch {
		case carried && !ran:
			t.Errorf("%s: go did not run", vector.ID)
		case !carried && ran:
			t.Errorf("%s: go ran a vector it cannot represent", vector.ID)
		case ran && actual.String() != vector.Expect.String():
			t.Errorf("%s: go produced %s, want %s", vector.ID, actual, vector.Expect)
		}
	}
	for _, planError := range document.PlanErrors {
		if message := goResults[planError.ID].Reason; !strings.Contains(message, planError.Expect) {
			t.Errorf("%s: Go plan error %q lacks %q", planError.ID, message, planError.Expect)
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

// codecVectorGoPlan is a vector plan for the runtime engine's validator,
// with inert Construct adapters: plan errors never reach a carrier.
func codecVectorGoPlan(plan codecVectorPlan, profile string) rt.CodecPlan {
	construct := func(int, []any) any { panic("plan errors construct no carrier") }
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
			compiled.Construct = construct
		case rt.CodecUnion:
			compiled.Variants = []rt.CodecVariant{}
			for _, variant := range node.Variants {
				compiled.Variants = append(compiled.Variants, rt.CodecVariant{Tag: variant.Tag, Fields: fields(variant.Fields)})
			}
			compiled.Construct = construct
		case rt.CodecTransform:
			compiled.Child, compiled.Step = node.Child, node.Step
		}
		out.Nodes = append(out.Nodes, compiled)
	}
	return out
}

// runCodecVectorsGo returns outcomes by vector id, from the generated Go
// path, and plan-error messages (as Reason) by plan-error id.
func runCodecVectorsGo(t *testing.T, document codecVectorDocument) map[string]codecOutcome {
	t.Helper()
	jobs := []codecVectorJob{}
	for _, vector := range document.Vectors {
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
			jobs = append(jobs, codecVectorJob{id: vector.ID, plan: vector.Plan, body: body, decode: true})
		case slices.Contains(vector.Targets, "go"):
			var value any
			if err := json.Unmarshal(vector.Value, &value); err != nil {
				t.Fatalf("%s: %v", vector.ID, err)
			}
			jobs = append(jobs, codecVectorJob{id: vector.ID, plan: vector.Plan, value: value})
		}
	}
	results := runCodecVectorsGenerated(t, document.Profile, document.Plans, jobs)
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
