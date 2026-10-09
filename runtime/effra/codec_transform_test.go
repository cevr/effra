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
			},
			{Kind: CodecTransform, Type: "witness:emailJson", Child: 0, Step: 0},
			{
				Kind: CodecRecord, Type: "Contact",
				Fields:    []CodecField{{"email", 2}},
				Construct: func(_ int, f []any) any { return codecTestContact{Email: f[0].(codecTestEmail)} },
			},
			{Kind: CodecBool},
		},
	}
}

var errCodecTestRejected = errors.New("rejected address")

// codecTestHalt stands in for the caller-owned outcome a step carries.
type codecTestHalt struct{ tag string }

func (h *codecTestHalt) Error() string { return "halted: " + h.tag }

// codecTestProfileSteps is the typed encode step a generated writer for
// codecTestProfilePlan takes.
type codecTestProfileSteps struct {
	email func(codecTestEmail) (string, error)
}

func codecTestWriteEmail(w *CodecWriter, s *codecTestProfileSteps, v *codecTestEmail) error {
	represented, err := s.email(*v)
	if err != nil {
		return err
	}
	w.String(represented)
	return nil
}

func codecTestWriteContact(w *CodecWriter, s *codecTestProfileSteps, v *codecTestContact) error {
	w.Raw(`{"email":`)
	w.Enter("email")
	if err := codecTestWriteEmail(w, s, &v.Email); err != nil {
		return err
	}
	w.Leave()
	w.Raw(`}`)
	return nil
}

func codecTestWriteProfile(w *CodecWriter, s *codecTestProfileSteps, v *codecTestProfile) error {
	w.Raw(`{"id":`)
	w.Enter("id")
	w.String(v.ID)
	w.Leave()
	w.Raw(`,"email":`)
	w.Enter("email")
	if err := codecTestWriteEmail(w, s, &v.Email); err != nil {
		return err
	}
	w.Leave()
	w.Raw(`,"contact":`)
	w.Enter("contact")
	if err := codecTestWriteContact(w, s, &v.Contact); err != nil {
		return err
	}
	w.Leave()
	w.Raw(`,"active":`)
	w.Enter("active")
	w.Bool(v.Active)
	w.Leave()
	w.Raw(`}`)
	return nil
}

func codecTestEncodeProfile(codec *Codec, value codecTestProfile, steps codecTestProfileSteps) ([]byte, error) {
	w := codec.Writer()
	return w.Finish(codecTestWriteProfile(w, &steps, &value))
}

// codecTestEmailSteps normalizes on decode (lower case) and refuses an
// unrepresentable domain value on encode, recording every call in order.
func codecTestEmailSteps(calls *[]string) (decode []CodecStep, encode codecTestProfileSteps) {
	decode = []CodecStep{func(value any) (any, error) {
		text := value.(string)
		*calls = append(*calls, "decode:"+text)
		if text == "" {
			return nil, &codecTestHalt{tag: "InvalidEmail"}
		}
		return codecTestEmail{Address: strings.ToLower(text)}, nil
	}}
	encode = codecTestProfileSteps{email: func(email codecTestEmail) (string, error) {
		*calls = append(*calls, "encode:"+email.Address)
		if email.Address == "blocked" {
			return "", errCodecTestRejected
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
	encoded, err := codecTestEncodeProfile(codec, value.(codecTestProfile), encode)
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
	if _, err := codecTestEncodeProfile(codec, value, encode); err != errCodecTestRejected {
		t.Fatalf("encode step refusal not returned unchanged: %v", err)
	}
	if !reflect.DeepEqual(calls, []string{"encode:a", "encode:blocked"}) {
		t.Fatalf("encode steps ran as %v", calls)
	}
	value.Contact.Email.Address = "b"
	var structural *CodecError
	if _, err := codecTestEncodeProfile(codec, value, encode); !errors.As(err, &structural) || structural.Reason != CodecInvalidUnicode || !reflect.DeepEqual(structural.Path, []string{"id"}) {
		t.Fatalf("structural encode failure after transformations: %v", err)
	}
	// The representation is bounded like any other output.
	value.ID = strings.Repeat("x", 300)
	if _, err := codecTestEncodeProfile(codec, value, encode); !errors.As(err, &structural) || structural.Reason != CodecBodyTooLarge {
		t.Fatalf("bounded transformed output: %v", err)
	}
}

func TestCodecStepCountIsACompilerContract(t *testing.T) {
	codec := mustCompileCodec(t, codecTestProfilePlan())
	for name, run := range map[string]func(){
		"decode without steps": func() { _, _ = codec.Decode([]byte(`{}`)) },
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
