package effra

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// codecTestEmail is a nominal domain value whose wire representation is a
// plain string; codecTestContact repeats it below a second path.
type codecTestEmail struct{ Address string }
type codecTestContact struct{ Email codecTestEmail }
type codecTestProfile struct {
	ID      string
	Email   codecTestEmail
	Contact codecTestContact
	Active  bool
}

// codecTestProfilePlan shares one transform node (Email over string) between
// Profile.email and Profile.contact.email.
func codecTestProfilePlan() CodecPlan {
	return CodecPlan{
		Profile: CodecProfileJSON,
		Bounds:  CodecBounds{MaxBodyBytes: 256, MaxDepth: 2},
		Root:    1,
		Nodes: []CodecNode{
			{Kind: CodecString},
			{
				Kind: CodecRecord, Type: "Profile",
				Fields: []CodecField{{"id", 0}, {"email", 2}, {"contact", 3}, {"active", 4}},
				Construct: func(_ int, f []any) any {
					return codecTestProfile{ID: f[0].(string), Email: f[1].(codecTestEmail), Contact: f[2].(codecTestContact), Active: f[3].(bool)}
				},
				Project: func(v any) (int, []any) {
					p := v.(codecTestProfile)
					return 0, []any{p.ID, p.Email, p.Contact, p.Active}
				},
			},
			{Kind: CodecTransform, Type: "witness:emailJson", Child: 0, Step: 0},
			{
				Kind: CodecRecord, Type: "Contact",
				Fields:    []CodecField{{"email", 2}},
				Construct: func(_ int, f []any) any { return codecTestContact{Email: f[0].(codecTestEmail)} },
				Project:   func(v any) (int, []any) { return 0, []any{v.(codecTestContact).Email} },
			},
			{Kind: CodecBool},
		},
	}
}

var errCodecTestRejected = errors.New("rejected address")

// codecTestHalt stands in for the caller-owned outcome a step carries.
type codecTestHalt struct{ tag string }

func (h *codecTestHalt) Error() string { return "halted: " + h.tag }

// codecTestEmailSteps normalizes on decode (lower case) and refuses an
// unrepresentable domain value on encode, recording every call in order.
func codecTestEmailSteps(calls *[]string) (decode, encode []CodecStep) {
	decode = []CodecStep{func(value any) (any, error) {
		text := value.(string)
		*calls = append(*calls, "decode:"+text)
		if text == "" {
			return nil, &codecTestHalt{tag: "InvalidEmail"}
		}
		return codecTestEmail{Address: strings.ToLower(text)}, nil
	}}
	encode = []CodecStep{func(value any) (any, error) {
		email := value.(codecTestEmail)
		*calls = append(*calls, "encode:"+email.Address)
		if email.Address == "blocked" {
			return nil, errCodecTestRejected
		}
		return email.Address, nil
	}}
	return decode, encode
}

func TestCodecTransformsRunAfterStructureInDeclaredOrder(t *testing.T) {
	codec := mustCompileCodec(t, codecTestProfilePlan())
	if codec.Steps() != 1 {
		t.Fatalf("steps = %d", codec.Steps())
	}
	var calls []string
	decode, encode := codecTestEmailSteps(&calls)
	value, err := codec.DecodeSteps([]byte(`{"active":true,"contact":{"email":"B@X"},"email":"A@X","id":"7"}`), decode)
	want := codecTestProfile{ID: "7", Email: codecTestEmail{"a@x"}, Contact: codecTestContact{codecTestEmail{"b@x"}}, Active: true}
	if err != nil || !reflect.DeepEqual(value, want) {
		t.Fatalf("decoded %#v %v", value, err)
	}
	if !reflect.DeepEqual(calls, []string{"decode:A@X", "decode:B@X"}) {
		t.Fatalf("decode steps ran as %v", calls)
	}
	calls = nil
	encoded, err := codec.EncodeSteps(value, encode)
	if err != nil || string(encoded) != `{"id":"7","email":"a@x","contact":{"email":"b@x"},"active":true}` {
		t.Fatalf("encoded %s %v", encoded, err)
	}
	if !reflect.DeepEqual(calls, []string{"encode:a@x", "encode:b@x"}) {
		t.Fatalf("encode steps ran as %v", calls)
	}
}

func TestCodecMalformedInputNeverReachesATransform(t *testing.T) {
	codec := mustCompileCodec(t, codecTestProfilePlan())
	var calls []string
	decode, _ := codecTestEmailSteps(&calls)
	// The email transformation would refuse "", but the later structural
	// failure wins: decoding validates the whole structure first.
	_, err := codec.DecodeSteps([]byte(`{"id":"7","email":"","contact":{"email":"b"},"active":"yes"}`), decode)
	var structural *CodecError
	if !errors.As(err, &structural) || structural.Reason != CodecType || !reflect.DeepEqual(structural.Path, []string{"active"}) {
		t.Fatalf("structural failure was not reported first: %v", err)
	}
	if len(calls) != 0 {
		t.Fatalf("a transformation ran on malformed input: %v", calls)
	}
	_, err = codec.DecodeSteps([]byte(`{"id":"7","email":"","contact":{"email":"b"},"active":true}`), decode)
	var halt *codecTestHalt
	if !errors.As(err, &halt) || halt.tag != "InvalidEmail" || errors.As(err, &structural) {
		t.Fatalf("a transformation's failure was not returned unchanged: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"decode:"}) {
		t.Fatalf("decoding continued after the first halted transformation: %v", calls)
	}
}

func TestCodecEncodeTransformsPrecedeStructuralEncoding(t *testing.T) {
	codec := mustCompileCodec(t, codecTestProfilePlan())
	var calls []string
	_, encode := codecTestEmailSteps(&calls)
	// id is ill-formed text, but transformations run before any byte is
	// written, so the second email's refusal is the reported outcome.
	value := codecTestProfile{ID: "\xff", Email: codecTestEmail{"a"}, Contact: codecTestContact{codecTestEmail{"blocked"}}}
	if _, err := codec.EncodeSteps(value, encode); err != errCodecTestRejected {
		t.Fatalf("encode step refusal not returned unchanged: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"encode:a", "encode:blocked"}) {
		t.Fatalf("encode steps ran as %v", calls)
	}
	value.Contact.Email.Address = "b"
	var structural *CodecError
	if _, err := codec.EncodeSteps(value, encode); !errors.As(err, &structural) || structural.Reason != CodecInvalidUnicode || !reflect.DeepEqual(structural.Path, []string{"id"}) {
		t.Fatalf("structural encode failure after transformations: %v", err)
	}
	// The representation is bounded like any other output.
	value.ID = strings.Repeat("x", 300)
	if _, err := codec.EncodeSteps(value, encode); !errors.As(err, &structural) || structural.Reason != CodecBodyTooLarge {
		t.Fatalf("bounded transformed output: %v", err)
	}
}

func TestCodecStepCountIsACompilerContract(t *testing.T) {
	codec := mustCompileCodec(t, codecTestProfilePlan())
	for name, run := range map[string]func(){
		"decode without steps": func() { _, _ = codec.Decode([]byte(`{}`)) },
		"encode without steps": func() { _, _ = codec.Encode(codecTestProfile{}) },
		"extra decode step":    func() { _, _ = codec.DecodeSteps([]byte(`{}`), make([]CodecStep, 2)) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("a step count mismatch did not panic")
				}
			}()
			run()
		})
	}
}

func TestCodecTransformPlanValidation(t *testing.T) {
	cases := map[string]struct {
		edit func(*CodecPlan)
		want string
	}{
		"missing child": {func(p *CodecPlan) { p.Nodes[2].Child = 42 }, "missing node 42"},
		"step range":    {func(p *CodecPlan) { p.Nodes[2].Step = 42 }, "out of range"},
		"sparse steps":  {func(p *CodecPlan) { p.Nodes[2].Step = 1 }, "not dense"},
		"shared step": {func(p *CodecPlan) {
			p.Nodes = append(p.Nodes, CodecNode{Kind: CodecTransform, Type: "witness:other", Child: 0, Step: 0})
			p.Nodes[3].Fields[0].Node = 5
		}, "share step"},
		"anonymous witness": {func(p *CodecPlan) { p.Nodes[2].Type = "" }, "witness identity"},
		"own structure":     {func(p *CodecPlan) { p.Nodes[2].Fields = []CodecField{{"x", 0}} }, "no structure"},
		"copied witness": {func(p *CodecPlan) {
			p.Nodes = append(p.Nodes, CodecNode{Kind: CodecTransform, Type: "witness:emailJson", Child: 0, Step: 1})
			p.Nodes[3].Fields[0].Node = 5
		}, "repeat witness"},
		"recursion":         {func(p *CodecPlan) { p.Nodes[2].Child = 3 }, "recursive layout"},
		"primitive step":    {func(p *CodecPlan) { p.Nodes[0].Step = 1 }, "nominal structure"},
		"nominal reference": {func(p *CodecPlan) { p.Nodes[3].Child = 2 }, "transform structure"},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			plan := codecTestProfilePlan()
			testCase.edit(&plan)
			if _, err := CompileCodec(plan); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("malformed plan was not rejected with %q: %v", testCase.want, err)
			}
		})
	}
	// A transform node is not a container: the plan's nesting is Profile and
	// Contact only.
	plan := codecTestProfilePlan()
	plan.Bounds.MaxDepth = 2
	if _, err := CompileCodec(plan); err != nil {
		t.Fatalf("control plan rejected: %v", err)
	}
}

// codecTestShared is a level of a balanced binary value whose two children
// are one shared value, so a value of 2*levels small objects in memory
// encodes to 2^levels transformed leaves.
type codecTestShared struct{ fields []any }

// codecTestSharedPlan has one record node per level (Level1..levels, both
// fields naming the level below) over one transform node of a string leaf.
func codecTestSharedPlan(levels, maxBodyBytes int) (CodecPlan, any) {
	plan := CodecPlan{
		Profile: CodecProfileJSON,
		Bounds:  CodecBounds{MaxBodyBytes: maxBodyBytes, MaxDepth: levels + 1},
		Nodes: []CodecNode{
			{Kind: CodecString},
			{Kind: CodecTransform, Type: "witness:leafJson", Child: 0, Step: 0},
		},
	}
	var value any = "x"
	for level := 1; level <= levels; level++ {
		below := len(plan.Nodes) - 1
		plan.Nodes = append(plan.Nodes, CodecNode{
			Kind: CodecRecord, Type: "Level" + string(rune('A'+level)),
			Fields:    []CodecField{{"a", below}, {"b", below}},
			Construct: func(_ int, f []any) any { return codecTestShared{fields: f} },
			Project:   func(v any) (int, []any) { return 0, v.(codecTestShared).fields },
		})
		value = codecTestShared{fields: []any{value, value}}
	}
	plan.Root = len(plan.Nodes) - 1
	return plan, value
}

// Encoding a shared value retains nothing beyond the bounded output: with a
// 64-byte budget and 2^16 transformed leaves, every transformation still
// runs in order (the steps are those of running every one first), the
// result is body-too-large, and the pass allocates a bounded amount
// independent of the value's expansion (preparing a full tree allocated a
// container per expanded record). A failing last step still takes
// precedence over the deferred body-too-large. Positive control: the same
// plan over three levels fits a 128-byte budget and encodes exactly.
func TestCodecEncodeRetainsNoPreparedTree(t *testing.T) {
	const levels = 16
	plan, value := codecTestSharedPlan(levels, 64)
	codec, err := CompileCodec(plan)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	steps := []CodecStep{func(v any) (any, error) { calls++; return v, nil }}
	_, err = codec.EncodeSteps(value, steps)
	var failure *CodecError
	if !errors.As(err, &failure) || failure.Reason != CodecBodyTooLarge || calls != 1<<levels {
		t.Fatalf("shared value: %v after %d of %d steps", err, calls, 1<<levels)
	}
	if allocations := testing.AllocsPerRun(3, func() { _, _ = codec.EncodeSteps(value, steps) }); allocations > 64 {
		t.Fatalf("encoding a 64-byte budget allocated %.0f times for %d leaves", allocations, 1<<levels)
	}
	calls = 0
	last := []CodecStep{func(v any) (any, error) {
		calls++
		if calls == 1<<levels {
			return nil, errCodecTestRejected
		}
		return v, nil
	}}
	if _, err := codec.EncodeSteps(value, last); !errors.Is(err, errCodecTestRejected) {
		t.Fatalf("a failed step after the budget was exceeded: %v", err)
	}
	small, smallValue := codecTestSharedPlan(3, 128)
	smallCodec, err := CompileCodec(small)
	if err != nil {
		t.Fatal(err)
	}
	output, err := smallCodec.EncodeSteps(smallValue, steps)
	leaf := `{"a":"x","b":"x"}`
	pair := `{"a":` + leaf + `,"b":` + leaf + `}`
	if want := `{"a":` + pair + `,"b":` + pair + `}`; err != nil || string(output) != want {
		t.Fatalf("fitting control: %s %v, want %s", output, err, want)
	}
}
