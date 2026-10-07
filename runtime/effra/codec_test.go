package effra

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// These carriers mirror the native declarations the compiler emits for
// records, closed enums and payload failures; the adapters below are the
// small per-type functions a derived plan supplies.
type codecTestPerson struct {
	Name  string
	Email string
}

type codecTestState interface{ codecTestStateVariant() }
type codecTestIdle struct{}
type codecTestRunning struct {
	RunID string
	Owner codecTestPerson
}

func (codecTestIdle) codecTestStateVariant()    {}
func (codecTestRunning) codecTestStateVariant() {}

type codecTestAccount struct {
	ID     int64
	Owner  codecTestPerson
	State  codecTestState
	Active bool
	Note   struct{}
}

type codecTestNotFound struct {
	ID       int64
	Resource string
}

func codecTestAccountPlan(bounds CodecBounds) CodecPlan {
	return CodecPlan{
		Profile: CodecProfileJSON,
		Bounds:  bounds,
		Root:    4,
		Nodes: []CodecNode{
			{Kind: CodecString},
			{Kind: CodecBool},
			{Kind: CodecI64},
			{Kind: CodecVoid},
			{
				Kind: CodecRecord, Type: "Account",
				Fields: []CodecField{{"id", 2}, {"owner", 5}, {"state", 6}, {"active", 1}, {"note", 3}},
				Construct: func(_ int, f []any) any {
					return codecTestAccount{ID: f[0].(int64), Owner: f[1].(codecTestPerson), State: f[2].(codecTestState), Active: f[3].(bool), Note: f[4].(struct{})}
				},
				Project: func(v any) (int, []any) {
					a := v.(codecTestAccount)
					return 0, []any{a.ID, a.Owner, a.State, a.Active, a.Note}
				},
			},
			{
				Kind: CodecRecord, Type: "Person",
				Fields: []CodecField{{"name", 0}, {"email", 0}},
				Construct: func(_ int, f []any) any {
					return codecTestPerson{Name: f[0].(string), Email: f[1].(string)}
				},
				Project: func(v any) (int, []any) {
					p := v.(codecTestPerson)
					return 0, []any{p.Name, p.Email}
				},
			},
			{
				Kind: CodecUnion, Type: "State",
				Variants: []CodecVariant{{Tag: "Idle"}, {Tag: "Running", Fields: []CodecField{{"runId", 0}, {"owner", 5}}}},
				Construct: func(variant int, f []any) any {
					if variant == 0 {
						return codecTestIdle{}
					}
					return codecTestRunning{RunID: f[0].(string), Owner: f[1].(codecTestPerson)}
				},
				Project: func(v any) (int, []any) {
					switch s := v.(type) {
					case codecTestIdle:
						return 0, nil
					case codecTestRunning:
						return 1, []any{s.RunID, s.Owner}
					}
					panic("unknown State variant")
				},
			},
		},
	}
}

func codecTestFailurePlan() CodecPlan {
	return CodecPlan{
		Profile: CodecProfileJSON,
		Bounds:  CodecBounds{MaxBodyBytes: 256, MaxDepth: 2},
		Root:    2,
		Nodes: []CodecNode{
			{Kind: CodecI64},
			{Kind: CodecString},
			{
				Kind: CodecUnion, Type: "NotFound",
				Variants:  []CodecVariant{{Tag: "NotFound", Fields: []CodecField{{"id", 0}, {"resource", 1}}}},
				Construct: func(_ int, f []any) any { return codecTestNotFound{ID: f[0].(int64), Resource: f[1].(string)} },
				Project: func(v any) (int, []any) {
					n := v.(codecTestNotFound)
					return 0, []any{n.ID, n.Resource}
				},
			},
		},
	}
}

func mustCompileCodec(t *testing.T, plan CodecPlan) *Codec {
	t.Helper()
	codec, err := CompileCodec(plan)
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func TestCodecRoundTripsGeneratedCarriers(t *testing.T) {
	codec := mustCompileCodec(t, codecTestAccountPlan(CodecBounds{MaxBodyBytes: 4096, MaxDepth: 4}))
	ada := codecTestPerson{Name: "Ada", Email: "ada@example.test"}
	body := `{"extra":[1,{"x":null}],"note":null,"active":true,"state":{"owner":{"email":"ada@example.test","name":"Ada"},"runId":"r-1","_tag":"Running"},"owner":{"name":"Ada","email":"ada@example.test"},"id":"-0042"}`
	value, err := codec.Decode([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	want := codecTestAccount{ID: -42, Owner: ada, State: codecTestRunning{RunID: "r-1", Owner: ada}, Active: true}
	if !reflect.DeepEqual(value, want) {
		t.Fatalf("decoded %#v, want %#v", value, want)
	}
	encoded, err := codec.Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	canonical := `{"id":"-42","owner":{"name":"Ada","email":"ada@example.test"},"state":{"_tag":"Running","runId":"r-1","owner":{"name":"Ada","email":"ada@example.test"}},"active":true,"note":null}`
	if string(encoded) != canonical {
		t.Fatalf("encoded %s, want %s", encoded, canonical)
	}
	again, err := codec.Decode(encoded)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatalf("canonical bytes did not round-trip: %#v %v", again, err)
	}

	idle := codecTestAccount{ID: 1, Owner: ada, State: codecTestIdle{}}
	encoded, err = codec.Encode(idle)
	if err != nil || !strings.Contains(string(encoded), `"state":{"_tag":"Idle"}`) {
		t.Fatalf("payload-free variant: %s %v", encoded, err)
	}

	failure := mustCompileCodec(t, codecTestFailurePlan())
	value, err = failure.Decode([]byte(`{"resource":"user","_tag":"NotFound","id":"7"}`))
	if err != nil || value != (codecTestNotFound{ID: 7, Resource: "user"}) {
		t.Fatalf("payload failure decode: %#v %v", value, err)
	}
	encoded, err = failure.Encode(value)
	if err != nil || string(encoded) != `{"_tag":"NotFound","id":"7","resource":"user"}` {
		t.Fatalf("payload failure encode: %s %v", encoded, err)
	}
}

func TestCodecDecodeFailuresUseDeclaredOrderAndPaths(t *testing.T) {
	codec := mustCompileCodec(t, codecTestAccountPlan(CodecBounds{MaxBodyBytes: 4096, MaxDepth: 4}))
	cases := []struct {
		body   string
		reason CodecReason
		path   []string
		offset int
	}{
		// Field order in the input does not choose the reported failure.
		{`{"note":1,"id":"x"}`, CodecInteger, []string{"id"}, -1},
		{`{"id":"1"}`, CodecMissing, []string{"owner"}, -1},
		{`{"id":"1","owner":{"name":"a","email":null}}`, CodecType, []string{"owner", "email"}, -1},
		{`{"id":"1","owner":{"name":"a","email":"b"},"state":{"_tag":"Paused"}}`, CodecTag, []string{"state"}, -1},
		{`{"id":"1","owner":{"name":"a","email":"b"},"state":{"runId":"r"}}`, CodecTag, []string{"state"}, -1},
		{`{"id":"1","owner":{"name":"a","email":"b"},"state":{"_tag":"Running","runId":"r"}}`, CodecMissing, []string{"state", "owner"}, -1},
		{`{"id":"9223372036854775808"}`, CodecRange, []string{"id"}, -1},
		{`[]`, CodecType, []string{}, -1},
		{`{"id":"1","id":"2"}`, CodecDuplicateKey, []string{}, 10},
		{`{"a":{"b":{"c":{"d":{}}}}}`, CodecDepth, []string{}, 20},
	}
	for _, testCase := range cases {
		_, err := codec.Decode([]byte(testCase.body))
		var failure *CodecError
		if !errors.As(err, &failure) {
			t.Fatalf("%s: expected CodecError, got %v", testCase.body, err)
		}
		want := &CodecError{Direction: CodecDecode, Reason: testCase.reason, Path: testCase.path, Offset: testCase.offset}
		if !reflect.DeepEqual(failure, want) {
			t.Fatalf("%s: failure %#v, want %#v", testCase.body, failure, want)
		}
	}
}

func TestCodecFailuresNeverEchoInput(t *testing.T) {
	codec := mustCompileCodec(t, codecTestAccountPlan(CodecBounds{MaxBodyBytes: 4096, MaxDepth: 4}))
	secret := "s3cr3t-" + strings.Repeat("x", 200)
	for _, body := range []string{
		`{"id":"` + secret + `"}`,
		`{"id":"1","owner":{"name":{"` + secret + `":1},"email":"b"}}`,
		`{"` + secret + `":1,"` + secret + `":2}`,
		`{"id":"1",` + secret,
	} {
		_, err := codec.Decode([]byte(body))
		if err == nil || strings.Contains(err.Error(), "s3cr3t") || len(err.Error()) > 120 {
			t.Fatalf("failure is unbounded or echoes input: %v", err)
		}
	}
}

func TestCodecEncodeFailuresAreTyped(t *testing.T) {
	codec := mustCompileCodec(t, codecTestAccountPlan(CodecBounds{MaxBodyBytes: 4096, MaxDepth: 4}))
	invalid := codecTestAccount{ID: 1, Owner: codecTestPerson{Name: "a\xffb", Email: "e"}, State: codecTestIdle{}}
	_, err := codec.Encode(invalid)
	want := &CodecError{Direction: CodecEncode, Reason: CodecInvalidUnicode, Path: []string{"owner", "name"}, Offset: -1}
	var failure *CodecError
	if !errors.As(err, &failure) || !reflect.DeepEqual(failure, want) {
		t.Fatalf("invalid UTF-8 encode: %#v", err)
	}

	value := codecTestAccount{ID: 1, Owner: codecTestPerson{Name: "a", Email: "e"}, State: codecTestIdle{}}
	full, err := codec.Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	exact := codecTestAccountPlan(CodecBounds{MaxBodyBytes: len(full), MaxDepth: 4})
	if encoded, err := mustCompileCodec(t, exact).Encode(value); err != nil || string(encoded) != string(full) {
		t.Fatalf("output at the exact bound was refused: %v", err)
	}
	short := codecTestAccountPlan(CodecBounds{MaxBodyBytes: len(full) - 1, MaxDepth: 4})
	_, err = mustCompileCodec(t, short).Encode(value)
	want = &CodecError{Direction: CodecEncode, Reason: CodecBodyTooLarge, Path: []string{}, Offset: -1}
	if !errors.As(err, &failure) || !reflect.DeepEqual(failure, want) {
		t.Fatalf("over-limit encode: %#v", err)
	}
	if _, err := mustCompileCodec(t, short).Decode(full); !errors.As(err, &failure) || failure.Reason != CodecBodyTooLarge {
		t.Fatalf("over-limit decode: %#v", err)
	}
}

// TestCodecEncodeConstructsNoOutputBeyondTheBound encodes megabyte strings,
// including ones whose escaped form is six times longer, under small bounds.
// The refusal must come from the allowance check while escaping, so the bytes
// Encode allocates stay a small multiple of MaxBodyBytes, not of the input.
func TestCodecEncodeConstructsNoOutputBeyondTheBound(t *testing.T) {
	inputs := map[string]string{
		"control": strings.Repeat("\x01", 1<<20),
		"quote":   strings.Repeat(`"`, 1<<20),
		"plain":   strings.Repeat("a", 1<<20),
		"scalar":  strings.Repeat("\u00e9", 1<<19),
	}
	for _, limit := range []int{32, 4096} {
		codec := mustCompileCodec(t, CodecPlan{Profile: CodecProfileJSON, Bounds: CodecBounds{MaxBodyBytes: limit, MaxDepth: 1}, Nodes: []CodecNode{{Kind: CodecString}}})
		for name, text := range inputs {
			var failure *CodecError
			if _, err := codec.Encode(text); !errors.As(err, &failure) || failure.Reason != CodecBodyTooLarge {
				t.Fatalf("%s under %d bytes: %#v", name, limit, err)
			}
			const runs = 8
			var before, after runtime.MemStats
			runtime.GC()
			runtime.ReadMemStats(&before)
			for range runs {
				_, _ = codec.Encode(text)
			}
			runtime.ReadMemStats(&after)
			if perRun := (after.TotalAlloc - before.TotalAlloc) / runs; perRun > uint64(4*limit+1024) {
				t.Errorf("%s under %d bytes allocated %d bytes per Encode", name, limit, perRun)
			}
		}
	}
}

func TestCodecAdapterMismatchIsADefect(t *testing.T) {
	value := codecTestAccount{Owner: codecTestPerson{Name: "a", Email: "e"}, State: codecTestIdle{}}
	cases := map[string]func(*CodecPlan){
		"field count":     func(p *CodecPlan) { p.Nodes[5].Project = func(any) (int, []any) { return 0, []any{"only-one"} } },
		"primitive carry": func(p *CodecPlan) { p.Nodes[5].Project = func(any) (int, []any) { return 0, []any{1, "e"} } },
		"variant index":   func(p *CodecPlan) { p.Nodes[6].Project = func(any) (int, []any) { return 2, nil } },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			plan := codecTestAccountPlan(CodecBounds{MaxBodyBytes: 4096, MaxDepth: 4})
			edit(&plan)
			codec := mustCompileCodec(t, plan)
			defer func() {
				if recovered := recover(); recovered == nil || !strings.HasPrefix(recovered.(string), "effra codec:") {
					t.Fatalf("adapter mismatch was not an engine defect: %v", recovered)
				}
			}()
			_, _ = codec.Encode(value)
		})
	}
}

func TestCodecSharedPlanNodesAvoidPathEnumeration(t *testing.T) {
	// Each level references the next twice: 2^levels paths, levels+1 nodes.
	const levels = 300
	plan := CodecPlan{Profile: CodecProfileJSON, Bounds: CodecBounds{MaxBodyBytes: 64, MaxDepth: levels}}
	identity := func(_ int, fields []any) any { return fields }
	project := func(value any) (int, []any) { return 0, value.([]any) }
	for level := 0; level < levels; level++ {
		plan.Nodes = append(plan.Nodes, CodecNode{
			Kind: CodecRecord, Type: "Level" + strconv.Itoa(level),
			Fields:    []CodecField{{"left", level + 1}, {"right", level + 1}},
			Construct: identity, Project: project,
		})
	}
	plan.Nodes = append(plan.Nodes, CodecNode{Kind: CodecBool})
	if _, err := CompileCodec(plan); err != nil {
		t.Fatalf("shared exponential-path plan was not compiled linearly: %v", err)
	}
	plan.Bounds.MaxDepth = levels - 1
	if _, err := CompileCodec(plan); err == nil || !strings.Contains(err.Error(), "nesting exceeds") {
		t.Fatalf("depth through shared nodes was not measured: %v", err)
	}
}

func TestCodecPlanValidationRejectsMalformedPlans(t *testing.T) {
	base := func() CodecPlan { return codecTestAccountPlan(CodecBounds{MaxBodyBytes: 4096, MaxDepth: 4}) }
	cases := map[string]struct {
		edit func(*CodecPlan)
		want string
	}{
		"profile":          {func(p *CodecPlan) { p.Profile = "effra/json-strict-1" }, "unsupported profile"},
		"body bound":       {func(p *CodecPlan) { p.Bounds.MaxBodyBytes = 0 }, "MaxBodyBytes"},
		"depth bound":      {func(p *CodecPlan) { p.Bounds.MaxDepth = CodecMaxDepth + 1 }, "MaxDepth"},
		"nesting":          {func(p *CodecPlan) { p.Bounds.MaxDepth = 2 }, "nesting exceeds"},
		"root":             {func(p *CodecPlan) { p.Root = 99 }, "root"},
		"empty":            {func(p *CodecPlan) { p.Nodes = nil }, "between 1 and"},
		"kind":             {func(p *CodecPlan) { p.Nodes[1].Kind = "f64" }, "unsupported kind"},
		"missing child":    {func(p *CodecPlan) { p.Nodes[5].Fields[1].Node = 42 }, "missing node 42"},
		"self recursion":   {func(p *CodecPlan) { p.Nodes[5].Fields[1].Node = 5 }, "recursive layout"},
		"mutual recursion": {func(p *CodecPlan) { p.Nodes[5].Fields[1].Node = 6 }, "recursive layout"},
		"copied primitive": {func(p *CodecPlan) {
			p.Nodes = append(p.Nodes, CodecNode{Kind: CodecString})
			p.Nodes[5].Fields[1].Node = 7
		}, "repeat primitive"},
		"copied type": {func(p *CodecPlan) {
			copied := p.Nodes[5]
			p.Nodes = append(p.Nodes, copied)
			p.Nodes[4].Fields[1].Node = 7
		}, "repeat type"},
		"unreachable": {func(p *CodecPlan) {
			p.Nodes = append(p.Nodes, CodecNode{Kind: CodecRecord, Type: "Orphan", Construct: p.Nodes[5].Construct, Project: p.Nodes[5].Project})
		}, "unreachable"},
		"duplicate field":    {func(p *CodecPlan) { p.Nodes[5].Fields[1].Name = "name" }, "repeats field"},
		"empty field":        {func(p *CodecPlan) { p.Nodes[5].Fields[1].Name = "" }, "field name"},
		"invalid field":      {func(p *CodecPlan) { p.Nodes[5].Fields[1].Name = "\xff" }, "field name"},
		"duplicate tag":      {func(p *CodecPlan) { p.Nodes[6].Variants[1].Tag = "Idle" }, "repeats tag"},
		"discriminator":      {func(p *CodecPlan) { p.Nodes[6].Variants[1].Fields[0].Name = CodecTagKey }, "discriminator"},
		"empty union":        {func(p *CodecPlan) { p.Nodes[6].Variants = nil }, "needs variants"},
		"record variants":    {func(p *CodecPlan) { p.Nodes[5].Variants = []CodecVariant{{Tag: "X"}} }, "has variants"},
		"missing adapter":    {func(p *CodecPlan) { p.Nodes[5].Project = nil }, "both adapters"},
		"anonymous nominal":  {func(p *CodecPlan) { p.Nodes[5].Type = "" }, "type identity"},
		"nominal primitive":  {func(p *CodecPlan) { p.Nodes[0].Type = "Name" }, "nominal structure"},
		"primitive adapters": {func(p *CodecPlan) { p.Nodes[0].Construct = p.Nodes[5].Construct }, "nominal structure"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			plan := base()
			testCase.edit(&plan)
			if _, err := CompileCodec(plan); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("malformed plan was not rejected with %q: %v", testCase.want, err)
			}
		})
	}
	if _, err := CompileCodec(base()); err != nil {
		t.Fatalf("control plan rejected: %v", err)
	}
}

func TestCodecCompiledPlanIsIsolatedFromCallerMutation(t *testing.T) {
	plan := codecTestAccountPlan(CodecBounds{MaxBodyBytes: 4096, MaxDepth: 4})
	codec := mustCompileCodec(t, plan)
	plan.Nodes[5].Fields[0].Name = "renamed"
	plan.Nodes[6].Variants[1].Tag = "Moved"
	value := codecTestAccount{ID: 1, Owner: codecTestPerson{Name: "a", Email: "e"}, State: codecTestRunning{RunID: "r", Owner: codecTestPerson{Name: "a", Email: "e"}}}
	encoded, err := codec.Encode(value)
	if err != nil || !strings.Contains(string(encoded), `{"_tag":"Running","runId":"r"`) || strings.Contains(string(encoded), "renamed") {
		t.Fatalf("compiled codec observed caller mutation: %s %v", encoded, err)
	}
}

func TestCodecConcurrentUseSharesOnlyImmutableState(t *testing.T) {
	codec := mustCompileCodec(t, codecTestAccountPlan(CodecBounds{MaxBodyBytes: 4096, MaxDepth: 4}))
	body := []byte(`{"id":"5","owner":{"name":"a","email":"e"},"state":{"_tag":"Running","runId":"r","owner":{"name":"b","email":"f"}},"active":false,"note":null}`)
	var group sync.WaitGroup
	failures := make(chan error, 32)
	for worker := 0; worker < 32; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for i := 0; i < 50; i++ {
				value, err := codec.Decode(body)
				if err != nil {
					failures <- err
					return
				}
				encoded, err := codec.Encode(value)
				if err != nil || string(encoded) != string(body) {
					failures <- errors.New("concurrent round trip changed bytes")
					return
				}
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
}

// Generated codec tables must not add package initialization or global
// registries to applications that select this module.
func TestCodecModuleHasNoPackageState(t *testing.T) {
	selected, err := SelectSources(RuntimeModuleCodec)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sourceNames(selected), []string{"codec.go", "codec_json.go"}) {
		t.Fatalf("codec closure retained unrelated runtime sources: %v", sourceNames(selected))
	}
	for name, data := range selected {
		file, err := parser.ParseFile(token.NewFileSet(), name, data, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			if general, ok := declaration.(*ast.GenDecl); ok && general.Tok == token.VAR {
				t.Fatalf("%s declares package state", name)
			}
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv == nil && function.Name.Name == "init" {
				t.Fatalf("%s declares package initialization", name)
			}
		}
	}
}
