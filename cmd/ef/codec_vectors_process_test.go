package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	rt "effra.local/prototype/runtime/effra"
)

// codecVectorWitnesses derive, from ordinary declarations, one witness per
// U3a policy-vector plan with the same bounds. Derived node order differs
// from the hand-written vector plans; behavior must not.
var codecVectorWitnesses = map[string]string{
	"account": "Json.codec<Account>(maxBodyBytes: 1024, maxDepth: 4)",
	"state":   "Json.codec<State>(maxBodyBytes: 512, maxDepth: 2)",
	"text":    "Json.codec<string>(maxBodyBytes: 256, maxDepth: 1)",
	"integer": "Json.codec<i64>(maxBodyBytes: 256, maxDepth: 1)",
	"nothing": "Json.codec<void>(maxBodyBytes: 64, maxDepth: 1)",
	"flag":    "Json.codec<bool>(maxBodyBytes: 64, maxDepth: 1)",
	"nested":  "Json.codec<Outer>(maxBodyBytes: 512, maxDepth: 3)",
	"label":   "Json.codec<Label>(maxBodyBytes: 32, maxDepth: 1)",
	"pair":    "Json.codec<Pair>(maxBodyBytes: 32, maxDepth: 1)",
}

// codecVectorTypes are the plan types of the vectors, named per the
// vector plans: their domain tags are State.Idle and State.Running.
const codecVectorTypes = `import Json "effra/json"
record Person { name: string, email: string }
enum State {
    Idle
    Running { runId: string, owner: Person }
}
record Account { id: i64, owner: Person, state: State, active: bool, note: void }
record Inner { value: string }
record Outer { inner: Inner }
record Label { name: string }
record Pair { first: string, second: string }
`

type codecVector struct {
	ID        string          `json:"id"`
	Plan      string          `json:"plan"`
	Direction string          `json:"direction"`
	Body      *string         `json:"body"`
	BodyHex   string          `json:"bodyHex"`
	Value     json.RawMessage `json:"value"`
	Targets   []string        `json:"targets"`
	Expect    struct {
		OK      bool     `json:"ok"`
		Encoded string   `json:"encoded"`
		Reason  string   `json:"reason"`
		Path    []string `json:"path"`
		Offset  int      `json:"offset"`
	} `json:"expect"`
}

// codecVectorValue renders a vector's encode value as Effra construction
// source of the plan's root type, or reports that source cannot spell it:
// ill-formed text exists only in host strings, and the literal grammar has
// no negative integers.
func codecVectorValue(plan string, raw json.RawMessage) (string, bool) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	text := func(value any) (string, bool) {
		s, ok := value.(string)
		quoted, _ := json.Marshal(s)
		return string(quoted), ok
	}
	object, _ := value.(map[string]any)
	switch plan {
	case "text":
		return text(value)
	case "nothing":
		_, ok := object["$void"]
		return "void", ok
	case "integer":
		digits, ok := object["$i64"].(string)
		return digits, ok && !strings.HasPrefix(digits, "-")
	case "label":
		name, ok := text(object["name"])
		return "Label { name: " + name + " }", ok
	case "pair":
		first, ok1 := text(object["first"])
		second, ok2 := text(object["second"])
		return "Pair { first: " + first + ", second: " + second + " }", ok1 && ok2
	}
	return "", false
}

// The U3a policy vectors run through derived witnesses in real .ef test
// programs on both targets: identical outcomes, encoded bytes and failure
// messages, each equal to the vector's expectation.
func TestDerivedCodecPolicyVectorsMatchAcrossGoAndJS(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	modules, err := filepath.Abs("../../node_modules")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../../conformance/codecs/json-structural-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Vectors []codecVector `json:"vectors"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	source.WriteString(codecVectorTypes)
	names := make([]string, 0, len(codecVectorWitnesses))
	for name := range codecVectorWitnesses {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		source.WriteString("derive " + name + "Json = " + codecVectorWitnesses[name] + "\n")
	}
	// A vector is selected when both targets apply, its plan is derivable
	// from declarations, and source can spell its input: a decode body is
	// text, and an encode value is constructible.
	expected := map[string]codecVector{}
	unspelled := []string{}
	for index, vector := range file.Vectors {
		if _, derived := codecVectorWitnesses[vector.Plan]; !derived || len(vector.Targets) == 1 {
			continue
		}
		if bytes, err := hex.DecodeString(vector.BodyHex); vector.BodyHex != "" && err == nil && utf8.Valid(bytes) {
			text := string(bytes)
			vector.Body = &text
		}
		input := ""
		switch {
		case vector.Direction == "decode" && vector.Body != nil:
			body, _ := json.Marshal(*vector.Body)
			input = "run " + vector.Plan + "Json.decode(" + string(body) + ")"
		case vector.Direction == "encode":
			value, ok := codecVectorValue(vector.Plan, vector.Value)
			if !ok {
				unspelled = append(unspelled, vector.ID)
				continue
			}
			input = value
		default:
			continue
		}
		test := fmt.Sprintf("test_vector_%d", index)
		encoded, _ := json.Marshal(vector.Expect.Encoded)
		source.WriteString("effect fn " + test + "() -> void raises { AssertionFailed, JsonDecodeFailure, JsonEncodeFailure } uses { Assert } {\n")
		source.WriteString("    let value = " + input + "\n    let encoded = run " + vector.Plan + "Json.encode(value)\n    run Assert.equalText(encoded, " + string(encoded) + ")\n}\n")
		expected[test] = vector
	}
	// Negative i64 values have no source literal; decode vectors cover them.
	if want := []string{"encode.order-and-tag", "encode.i64-min"}; !reflect.DeepEqual(unspelled, want) || len(expected) < 80 {
		t.Fatalf("selected %d vectors; unspelled %v", len(expected), unspelled)
	}
	path := filepath.Join(root, "vectors.ef")
	if err := os.WriteFile(path, []byte(source.String()), 0600); err != nil {
		t.Fatal(err)
	}
	type reason struct {
		Kind    string `json:"kind"`
		Tag     string `json:"tag"`
		Message string `json:"message"`
	}
	type outcome struct {
		Name    string   `json:"name"`
		Passed  bool     `json:"passed"`
		Reasons []reason `json:"reasons"`
	}
	reports := map[string]map[string]outcome{}
	for _, target := range []string{"go", "js"} {
		stdout, stderr, _ := runTestCLIDir(t, binary, root, "", "test", path, "--target", target)
		var report struct {
			Tests []outcome `json:"tests"`
		}
		if err := json.Unmarshal(stdout, &report); err != nil || len(report.Tests) != len(expected) {
			t.Fatalf("%s vector report: %v (%d tests)\nstdout=%s\nstderr=%s", target, err, len(report.Tests), stdout, stderr)
		}
		reports[target] = map[string]outcome{}
		for _, test := range report.Tests {
			reports[target][test.Name] = test
		}
	}
	for test, vector := range expected {
		goOutcome, jsOutcome := reports["go"][test], reports["js"][test]
		if !reflect.DeepEqual(goOutcome, jsOutcome) {
			t.Errorf("%s: Go and JS differ:\n%+v\n%+v", vector.ID, goOutcome, jsOutcome)
			continue
		}
		if vector.Expect.OK {
			if !goOutcome.Passed {
				t.Errorf("%s: want success %s, got %+v", vector.ID, vector.Expect.Encoded, goOutcome)
			}
			continue
		}
		path := vector.Expect.Path
		if path == nil {
			path = []string{}
		}
		tag := "JsonDecodeFailure"
		direction := rt.CodecDecode
		if vector.Direction == "encode" {
			tag, direction = "JsonEncodeFailure", rt.CodecEncode
		}
		message := (&rt.CodecError{Direction: direction, Reason: rt.CodecReason(vector.Expect.Reason), Path: path, Offset: vector.Expect.Offset}).Error()
		if want := []reason{{Kind: "failure", Tag: tag, Message: message}}; goOutcome.Passed || !reflect.DeepEqual(goOutcome.Reasons, want) {
			t.Errorf("%s: want %+v, got %+v", vector.ID, want, goOutcome)
		}
	}
}
