package compiler

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

const codecDomainSource = `import Convert "ergo/conversions"
import Json "ergo/json"

record User {
    id: i64
    name: string
    admin: bool
}

record Note {
    text: string
    marker: void
}

enum Event {
    Created { user: User }
    Renamed { user: User, note: Note }
    Closed
}

derive userJson = Json.codec<User>(maxBodyBytes: 4096, maxDepth: 512)
derive eventJson = Json.codec<Event>(maxBodyBytes: 1048576, maxDepth: 512)
derive eventArchive = Json.codec<Event>(maxBodyBytes: 1048576, maxDepth: 512)

effect fn roundTrip(text: string) -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
    let codec = Convert.witness(eventJson.decode, eventJson.encode)
    let event = run codec.decode(text)
    run codec.encode(event)
}

effect fn main() -> string raises { JsonDecodeFailure, JsonEncodeFailure } {
    let user = run userJson.decode("{\"id\":\"7\",\"name\":\"Ada\",\"admin\":true}")
    run userJson.encode(user)
}
`

func codecPlanJSON(t *testing.T, r *Result) string {
	t.Helper()
	encoded, err := json.Marshal(struct {
		Codecs []CodecInspection
		Plans  []*CodecPlan
	}{r.Codecs, r.CodecPlans})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestCodecDeriveSynthesizesCheckedDirectionFunctions(t *testing.T) {
	r := CompileFor(codecDomainSource, "go")
	if !r.Checked {
		t.Fatalf("go: %+v", r.Diagnostics)
	}
	if len(r.Codecs) != 3 {
		t.Fatalf("go: want three derived witnesses: %+v", r.Codecs)
	}
	user := r.Codecs[0]
	if user.Name != "userJson" || user.Derivation != "ergo/json.codec" || user.Profile != "ergo/json-structural-1" || user.Domain != "User" || user.Wire != "string" || user.DomainType == "" {
		t.Fatalf("go: user witness: %+v", user)
	}
	if !slices.Equal(user.Decode.Failures, []string{"JsonDecodeFailure"}) || !slices.Equal(user.Encode.Failures, []string{"JsonEncodeFailure"}) || len(user.Decode.Requirements) != 0 || len(user.Encode.Requirements) != 0 {
		t.Fatalf("go: direction rows must be independent and service-free: %+v", user)
	}
	for _, name := range []string{"userJson.decode", "userJson.encode", "eventJson.decode", "eventArchive.encode"} {
		symbol := r.Find(name)
		if symbol == nil || symbol.Contract.Callable == nil || symbol.Contract.Callable.Kind != "effect" {
			t.Fatalf("go: %s is not an ordinary checked effect function: %+v", name, symbol)
		}
	}
	decode := r.Find("userJson.decode").Contract.Callable
	if len(decode.Parameters) != 1 || decode.Parameters[0].Type != "string" || !slices.Equal(decode.Failures, []string{"JsonDecodeFailure"}) || len(decode.Requirements) != 0 {
		t.Fatalf("go: decode contract: %+v", decode)
	}
	// Two witnesses for one domain type with the same bounds share one
	// plan; different bounds are distinct plans.
	if r.Codecs[1].Plan != r.Codecs[2].Plan || r.Codecs[0].Plan == r.Codecs[1].Plan || len(r.CodecPlans) != 2 {
		t.Fatalf("go: plan sharing: %+v", r.Codecs)
	}
}

func TestCodecDerivationRefusals(t *testing.T) {
	header := "import Json \"ergo/json\"\nimport Data \"ergo/data\"\n"
	cases := []struct {
		name, source, code, message string
	}{
		{"callable field", "record Job { run: effect fn() -> void }\nderive c = Json.codec<Job>(maxBodyBytes: 1024, maxDepth: 8)", "EF138", "codec c cannot derive ergo/json-structural-1 for Job: effect fn() -> void at Job.run is a function or effect recipe"},
		{"nested host field", "record Inner { latch: Latch }\nrecord Outer { inner: Inner }\nderive c = Json.codec<Outer>(maxBodyBytes: 1024, maxDepth: 8)", "EF138", "Latch at Outer.inner.latch"},
		{"bytes", "derive c = Json.codec<bytes>(maxBodyBytes: 1024, maxDepth: 8)", "EF138", "bytes at bytes is not representable"},
		{"generic application", "record Maybe { value: Data.Option<string> }\nderive c = Json.codec<Maybe>(maxBodyBytes: 1024, maxDepth: 8)", "EF138", "Data.Option<string> at Maybe.value is generic data"},
		{"recursive layout", "enum List { Nil, Cons { tail: List } }\nderive c = Json.codec<List>(maxBodyBytes: 1024, maxDepth: 8)", "EF119", "recursive data layout is unsupported"},
		{"empty enum", "enum Never { }\nderive c = Json.codec<Never>(maxBodyBytes: 1024, maxDepth: 8)", "EF138", "has no variants"},
		{"nesting beyond maxDepth", "record A { b: B }\nrecord B { c: C }\nrecord C { text: string }\nderive c = Json.codec<A>(maxBodyBytes: 1024, maxDepth: 2)", "EF138", "plan nesting 3 exceeds maxDepth 2"},
		{"zero maxDepth", "derive c = Json.codec<string>(maxBodyBytes: 64, maxDepth: 0)", "EF138", "maxDepth must be between 1 and 512"},
		{"maxDepth above ceiling", "derive c = Json.codec<string>(maxBodyBytes: 64, maxDepth: 513)", "EF138", "maxDepth must be between 1 and 512"},
		{"zero maxBodyBytes", "derive c = Json.codec<string>(maxBodyBytes: 0, maxDepth: 1)", "EF138", "maxBodyBytes must be between 1"},
		{"no bounds names both", "derive c = Json.codec<string>", "EF138", "codec c requires the explicit bound maxBodyBytes; codec bounds have no default\ncodec c requires the explicit bound maxDepth; codec bounds have no default"},
		{"missing maxDepth", "derive c = Json.codec<string>(maxBodyBytes: 64)", "EF138", "codec c requires the explicit bound maxDepth"},
		{"missing maxBodyBytes", "derive c = Json.codec<string>(maxDepth: 4)", "EF138", "codec c requires the explicit bound maxBodyBytes"},
		{"empty bounds", "derive c = Json.codec<string>()", "EF138", "codec c requires the explicit bound maxBodyBytes"},
		{"unknown option", "derive c = Json.codec<string>(maxBodyBytes: 64, maxDepth: 1, maxItems: 4)", "EF138", "maxItems is unknown"},
		{"repeated option", "derive c = Json.codec<string>(maxBodyBytes: 64, maxDepth: 4, maxDepth: 5)", "EF138", "maxDepth is repeated"},
		// A failure declaration derives a failure witness, which encodes only
		// mapped endpoint failure bodies and has no direction functions.
		{"failure witness has no directions", "error Bad { text: string }\nderive c = Json.codec<Bad>(maxBodyBytes: 1024, maxDepth: 8)\neffect fn main() -> void { let f = c.decode }", "EF102", "unknown value c"},
		{"unknown type", "derive c = Json.codec<Missing>(maxBodyBytes: 1024, maxDepth: 8)", "EF102", "unknown or unsupported value type Missing"},
		{"generic without arguments", "record Box<T: type> { value: T }\nderive c = Json.codec<Box>(maxBodyBytes: 1024, maxDepth: 8)", "EF127", "generic type Box requires complete application arguments"},
		{"local generic application", "record Box<T: type> { value: T }\nderive c = Json.codec<Box<string>>(maxBodyBytes: 1024, maxDepth: 8)", "EF138", "Box<string> at Box<string> is generic data"},
		{"bundled generic without arguments", "derive c = Json.codec<Data.Option>(maxBodyBytes: 1024, maxDepth: 8)", "EF102", "unknown or unsupported value type Data.Option"},
		{"unimported derivation", "derive c = Yaml.codec<string>", "EF102", "unknown derivation Yaml.codec"},
		{"unknown derivation member", "derive c = Json.schema<string>", "EF126", "unknown bundled declaration ergo/json/schema"},
		{"not a derivation", "import Convert \"ergo/conversions\"\nrecord User { name: string }\nfn read(text: string) -> User { User { name: text } }\nderive c = Convert.witness<User>", "EF138", "Convert.witness is not a codec derivation"},
		{"duplicate witness name", "derive c = Json.codec<string>(maxBodyBytes: 1024, maxDepth: 8)\nderive c = Json.codec<bool>(maxBodyBytes: 1024, maxDepth: 8)", "EF101", "duplicate declaration c"},
		{"witness collides with function", "fn c() -> string { \"x\" }\nderive c = Json.codec<string>(maxBodyBytes: 1024, maxDepth: 8)", "EF101", "duplicate declaration c"},
		{"failure name collides", "error JsonDecodeFailure\nderive c = Json.codec<string>(maxBodyBytes: 1024, maxDepth: 8)", "EF101", "duplicate declaration JsonDecodeFailure"},
		{"undeclared decode failure", "derive c = Json.codec<string>(maxBodyBytes: 1024, maxDepth: 8)\neffect fn main() -> string { run c.decode(\"\\\"x\\\"\") }", "EF107", "undeclared failures: JsonDecodeFailure"},
		{"undeclared encode failure", "derive c = Json.codec<string>(maxBodyBytes: 1024, maxDepth: 8)\neffect fn main() -> string raises { JsonDecodeFailure } { run c.encode(\"x\") }", "EF107", "undeclared failures: JsonEncodeFailure"},
		{"wrong decode argument", "derive c = Json.codec<string>(maxBodyBytes: 1024, maxDepth: 8)\neffect fn main() -> string raises { JsonDecodeFailure } { run c.decode(true) }", "EF106", "argument must be string"},
		{"unknown direction", "derive c = Json.codec<string>(maxBodyBytes: 1024, maxDepth: 8)\neffect fn main() -> string { run c.parse(\"x\") }", "EF102", "unknown function or service method"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(header + tc.source)
			if r.Checked {
				t.Fatalf("source was admitted")
			}
			// A newline in message lists several diagnostics that must all appear.
			for _, message := range strings.Split(tc.message, "\n") {
				found := false
				for _, d := range r.Diagnostics {
					found = found || d.Code == tc.code && strings.Contains(d.Message, message)
				}
				if !found {
					t.Fatalf("want %s %q: %+v", tc.code, message, r.Diagnostics)
				}
			}
			if _, err := r.EmitGo(); err == nil {
				t.Fatal("refused source emitted")
			}
		})
	}
	if r := Compile("effect fn main() -> void raises { JsonDecodeFailure } { void }"); r.Checked || !hasCode(r, "EF102") {
		t.Fatalf("codec failures must require the ergo/json import: %+v", r.Diagnostics)
	}
}

// Derive declarations format as one line with canonical spacing, and the
// formatter's output is a fixed point that checks unchanged.
func TestCodecDeriveDeclarationsFormat(t *testing.T) {
	source := "import Json \"ergo/json\"\nrecord User { name: string }\nderive   userJson=Json.codec< User >( maxBodyBytes:4096,maxDepth : 4 )\nderive userArchive = Json.codec<User>(maxBodyBytes: 64, maxDepth: 1)\n"
	want := "import Json \"ergo/json\"\nrecord User {\n    name: string\n}\nderive userJson = Json.codec<User>(maxBodyBytes: 4096, maxDepth: 4)\nderive userArchive = Json.codec<User>(maxBodyBytes: 64, maxDepth: 1)\n"
	assertFormat(t, source, want)
	if r := Compile(want); !r.Checked || len(r.Codecs) != 2 || r.CodecPlans[0].Bounds != (CodecPlanBounds{MaxBodyBytes: 4096, MaxDepth: 4}) {
		t.Fatalf("formatted derive declarations: %+v %+v", r.Diagnostics, r.CodecPlans)
	}
}

func TestCodecDeriveFormattingSharesAngleFactsWithComparisons(t *testing.T) {
	source := "import Json \"ergo/json\"\nrecord User { name: string }\nenum Status { Ready Done }\nderive userJson = Json.codec< User >(maxBodyBytes: 4096, maxDepth: 4)\nderive statusJson = Json.codec< Status >(maxBodyBytes: 64, maxDepth: 1)\nfn ordinaryComparison(left: i64, right: i64) -> bool { left < right }\n"
	want := "import Json \"ergo/json\"\nrecord User {\n    name: string\n}\nenum Status {\n    Ready\n    Done\n}\nderive userJson = Json.codec<User>(maxBodyBytes: 4096, maxDepth: 4)\nderive statusJson = Json.codec<Status>(maxBodyBytes: 64, maxDepth: 1)\nfn ordinaryComparison(left: i64, right: i64) -> bool {\n    left < right\n}\n"
	assertFormat(t, source, want)
	r := Compile(want)
	if !r.Checked || len(r.Codecs) != 2 || len(r.CodecPlans) != 2 {
		t.Fatalf("formatted record/enum derives or comparison rejected: %+v %+v", r.Diagnostics, r.CodecPlans)
	}
}
