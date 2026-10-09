package compiler

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// codecTransformSource composes a pure nominal witness, an effectful witness
// whose directions need different services, and two parents that share the
// witnesses under the same and a different `with` scope.
const codecTransformSource = `import Json "effra/json"

error Refused
error Unknown
service Lookup {
    effect fn known(text: string) -> bool
}
service Journal {
    effect fn note(text: string) -> void raises { Refused }
}

record Email {
    text: string
}

fn parseEmail(text: string) -> Email {
    Email {
        text: text
    }
}

fn renderEmail(email: Email) -> string {
    email.text
}

derive emailJson = Json.codec<Email>(maxBodyBytes: 256, maxDepth: 1, decode: parseEmail, encode: renderEmail)

record Member {
    email: Email
}

record MemberWire {
    email: Email
}

effect fn resolveMember(wire: MemberWire) -> Member raises { Unknown } uses { Lookup } {
    if run Lookup.known(wire.email.text) {
        Member {
            email: wire.email
        }
    } else {
        fail Unknown {}
    }
}

effect fn recordMember(member: Member) -> MemberWire raises { Refused } uses { Journal } {
    run Journal.note(member.email.text)
    MemberWire {
        email: member.email
    }
}

derive memberJson = Json.codec<Member>(maxBodyBytes: 512, maxDepth: 2, decode: resolveMember, encode: recordMember) with emailJson

record Team {
    lead: Member
    contact: Email
    backup: Email
}

derive teamJson = Json.codec<Team>(maxBodyBytes: 2048, maxDepth: 3) with emailJson, memberJson
derive teamArchive = Json.codec<Team>(maxBodyBytes: 2048, maxDepth: 3) with memberJson, emailJson
derive teamPlain = Json.codec<Team>(maxBodyBytes: 2048, maxDepth: 3) with memberJson

effect fn writeTeam(team: Team) -> string raises { JsonEncodeFailure, Refused } uses { Journal } {
    run teamJson.encode(team)
}
`

func codecInspectionNamed(t *testing.T, r *Result, name string) CodecInspection {
	t.Helper()
	for _, codec := range r.Codecs {
		if codec.Name == name {
			return codec
		}
	}
	t.Fatalf("no codec %s: %+v", name, r.Codecs)
	return CodecInspection{}
}

// The direction functions carry exactly the rows of the transformations they
// run: decode never needs an encode service, and an encode-only caller
// provides only encode services.
func TestCodecTransformationRowsAreDirectionSpecific(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		r := CompileFor(codecTransformSource, target)
		if !r.Checked {
			t.Fatalf("%s: %+v", target, r.Diagnostics)
		}
		email := codecInspectionNamed(t, r, "emailJson")
		if email.Representation != "string" || !slices.Equal(email.Decode.Failures, []string{"JsonDecodeFailure"}) || len(email.Decode.Requirements) != 0 {
			t.Fatalf("%s: a pure transformation adds no row: %+v", target, email)
		}
		member := codecInspectionNamed(t, r, "memberJson")
		if member.Representation != "MemberWire" || !slices.Equal(member.With, []string{"emailJson"}) {
			t.Fatalf("%s: member witness: %+v", target, member)
		}
		if !slices.Equal(member.Decode.Failures, []string{"JsonDecodeFailure", "Unknown"}) || !slices.Equal(member.Decode.Requirements, []string{"Lookup"}) {
			t.Fatalf("%s: member decode rows: %+v", target, member.Decode)
		}
		if !slices.Equal(member.Encode.Failures, []string{"JsonEncodeFailure", "Refused"}) || !slices.Equal(member.Encode.Requirements, []string{"Journal"}) {
			t.Fatalf("%s: member encode rows: %+v", target, member.Encode)
		}
		team := codecInspectionNamed(t, r, "teamJson")
		if team.Representation != "" || !slices.Equal(team.With, []string{"emailJson", "memberJson"}) {
			t.Fatalf("%s: team witness: %+v", target, team)
		}
		decode := r.Find("teamJson.decode").Contract.Callable
		if !slices.Equal(decode.Failures, []string{"JsonDecodeFailure", "Unknown"}) || !slices.Equal(decode.Requirements, []string{"Lookup"}) {
			t.Fatalf("%s: team decode contract: %+v", target, decode)
		}
		// Transformations are listed in the order the direction runs them,
		// each at the first path where it applies, with its own rows.
		var steps []string
		for _, step := range team.Decode.Transformations {
			steps = append(steps, step.Witness+"@"+step.Path+":"+step.Domain+"<-"+step.Representation+":"+strings.Join(step.Requirements, ","))
		}
		if want := []string{"memberJson@Team.lead:Member<-MemberWire:Lookup", "emailJson@Team.lead.email:Email<-string:"}; !slices.Equal(steps, want) {
			t.Fatalf("%s: decode transformations %q, want %q", target, steps, want)
		}
		if encode := team.Encode.Transformations; len(encode) != 2 || encode[0].Function == team.Decode.Transformations[0].Function || !slices.Equal(encode[0].Requirements, []string{"Journal"}) {
			t.Fatalf("%s: encode transformations: %+v", target, encode)
		}
	}
}

// One transform node per witness and one structural node per (type, with
// scope): the plan stays a shared DAG, equal scopes share plans in either
// listing order, and a different scope is a different plan.
func TestCodecTransformationPlansShareNodesByScope(t *testing.T) {
	r := Compile(codecTransformSource)
	if !r.Checked {
		t.Fatalf("%+v", r.Diagnostics)
	}
	team, archive, plain := codecInspectionNamed(t, r, "teamJson"), codecInspectionNamed(t, r, "teamArchive"), codecInspectionNamed(t, r, "teamPlain")
	if team.Plan != archive.Plan || team.Plan == plain.Plan {
		t.Fatalf("plans by scope: %q %q %q", team.Plan, archive.Plan, plain.Plan)
	}
	var plan *CodecPlan
	for _, candidate := range r.CodecPlans {
		if candidate.ID == team.Plan {
			plan = candidate
		}
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var shape struct {
		Root  int
		Nodes []struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			Witness string `json:"witness"`
			Child   *int   `json:"child"`
			Step    *int   `json:"step"`
			Decode  string `json:"decode"`
			Encode  string `json:"encode"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(encoded, &shape); err != nil {
		t.Fatal(err)
	}
	transforms := map[string]int{}
	scoped := 0
	for _, node := range shape.Nodes {
		if node.Kind == "transform" {
			if node.Child == nil || node.Step == nil || node.Decode == "" || node.Encode == "" {
				t.Fatalf("transform node lacks its contract: %s", encoded)
			}
			transforms[node.Witness]++
		} else if node.Child != nil || node.Step != nil || node.Witness != "" {
			t.Fatalf("structural node carries transform fields: %s", encoded)
		}
		if strings.HasSuffix(node.ID, "@with:emailJson,memberJson") {
			scoped++
		}
	}
	// Team.contact, Team.backup and MemberWire.email share one emailJson
	// node; MemberWire is derived under memberJson's own scope.
	if len(transforms) != 2 || transforms["emailJson"] != 1 || transforms["memberJson"] != 1 || scoped != 1 {
		t.Fatalf("transform sharing: %v scoped %d: %s", transforms, scoped, encoded)
	}
}

// Callers see a transformed witness's rows as those of any function.
func TestCodecTransformationCallersNeedTheDirectionRows(t *testing.T) {
	cases := []struct {
		name, source, code, message string
	}{
		{"missing decode service", "effect fn readTeam(text: string) -> Team raises { JsonDecodeFailure, Unknown } { run teamJson.decode(text) }", "EF108", "Lookup"},
		{"undeclared decode failure", "effect fn readTeam(text: string) -> Team raises { JsonDecodeFailure } uses { Lookup } { run teamJson.decode(text) }", "EF107", "Unknown"},
		{"undeclared encode failure", "effect fn writeMember(member: Member) -> string raises { JsonEncodeFailure } uses { Journal } { run memberJson.encode(member) }", "EF107", "Refused"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(codecTransformSource + tc.source)
			found := false
			for _, d := range r.Diagnostics {
				found = found || d.Code == tc.code && strings.Contains(d.Message, tc.message)
			}
			if r.Checked || !found {
				t.Fatalf("want %s %q: %+v", tc.code, tc.message, r.Diagnostics)
			}
		})
	}
	// Positive control: the same callers check once they state the rows,
	// and an encode-only caller (writeTeam) never states Lookup.
	r := Compile(codecTransformSource + "effect fn readTeam(text: string) -> Team raises { JsonDecodeFailure, Unknown } uses { Lookup } { run teamJson.decode(text) }")
	if !r.Checked {
		t.Fatalf("%+v", r.Diagnostics)
	}
	if write := r.Find("writeTeam").Contract.Callable; slices.Contains(write.Requirements, "Lookup") {
		t.Fatalf("encode-only caller needs a decode service: %+v", write)
	}
}

func TestCodecTransformationRefusals(t *testing.T) {
	header := `import Json "effra/json"
record Email { text: string }
record Box { email: Email }
fn parseEmail(text: string) -> Email { Email { text: text } }
fn renderEmail(email: Email) -> string { email.text }
fn flag(email: Email) -> bool { true }
fn pair(a: string, b: string) -> Email { Email { text: a } }
fn name(text: string) -> string { text }
fn bytesEmail(raw: bytes) -> Email { Email { text: "x" } }
error Refused
effect fn emailBytes(email: Email) -> bytes raises { Refused } { fail Refused {} }
derive emailJson = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: parseEmail, encode: renderEmail)
derive plainEmail = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1)
derive nameJson = Json.codec<string>(maxBodyBytes: 64, maxDepth: 1, decode: name, encode: name)
`
	cases := []struct {
		name, source, code, message string
	}{
		{"decode without encode", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: parseEmail)", "EF138", "codec c option decode requires encode: a transformation states both directions"},
		{"encode without decode", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, encode: renderEmail)", "EF138", "codec c option encode requires decode"},
		{"repeated transformation", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: parseEmail, decode: parseEmail, encode: renderEmail)", "EF138", "codec c option decode is repeated"},
		{"integer transformation", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: 3, encode: renderEmail)", "EF138", "codec c option decode names a transformation function"},
		{"function bound", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: parseEmail)", "EF138", "codec c option maxDepth takes an integer bound"},
		{"unknown option names the transformation options", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, refine: parseEmail)", "EF138", "supported options are maxBodyBytes, maxDepth, decode and encode"},
		{"unknown function", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: nope, encode: renderEmail)", "EF102", "unknown function nope for codec c option decode"},
		{"two parameters", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: pair, encode: renderEmail)", "EF138", "decode transformation pair must be a first-order function of exactly one parameter"},
		{"decode returns another type", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: name, encode: renderEmail)", "EF138", "decode transformation name returns string, not the derived type Email"},
		{"encode takes another type", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: parseEmail, encode: name)", "EF138", "encode transformation name takes string, not the derived type Email"},
		{"encode returns another representation", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: parseEmail, encode: flag)", "EF138", "encode transformation flag returns bool, not the representation string that parseEmail decodes"},
		{"unrepresentable representation", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: bytesEmail, encode: emailBytes)", "EF138", "codec c cannot derive effra/json-structural-1 for Email: bytes at Email is not representable"},
		{"unknown witness", "derive c = Json.codec<Box>(maxBodyBytes: 64, maxDepth: 2) with missing", "EF102", "unknown codec witness missing in codec c"},
		{"repeated witness", "derive c = Json.codec<Box>(maxBodyBytes: 64, maxDepth: 2) with emailJson, emailJson", "EF138", "codec c with emailJson: is repeated"},
		{"self composition", "derive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: parseEmail, encode: renderEmail) with c", "EF138", "codec c with c: a witness cannot compose itself"},
		{"structural witness", "derive c = Json.codec<Box>(maxBodyBytes: 64, maxDepth: 2) with plainEmail", "EF138", "codec c with plainEmail: it has no transformation"},
		{"primitive domain witness", "derive c = Json.codec<Box>(maxBodyBytes: 64, maxDepth: 2) with nameJson", "EF138", "codec c with nameJson: it transforms string; with admits witnesses of record and enum types"},
		{"two witnesses for one domain", "derive other = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: parseEmail, encode: renderEmail)\nderive c = Json.codec<Box>(maxBodyBytes: 64, maxDepth: 2) with emailJson, other", "EF138", "codec c with other: codec emailJson already transforms Email"},
		{"unused witness", "record Plain { text: string }\nderive c = Json.codec<Plain>(maxBodyBytes: 64, maxDepth: 1) with emailJson", "EF138", "with entry emailJson is unused: Email does not occur in the representation of Plain"},
		{"recursion through witnesses", "record A { text: string }\nrecord B { text: string }\nrecord AWire { b: B }\nrecord BWire { a: A }\nfn readA(wire: AWire) -> A { A { text: wire.b.text } }\nfn writeA(a: A) -> AWire { AWire { b: B { text: a.text } } }\nfn readB(wire: BWire) -> B { B { text: wire.a.text } }\nfn writeB(b: B) -> BWire { BWire { a: A { text: b.text } } }\nderive aJson = Json.codec<A>(maxBodyBytes: 64, maxDepth: 8, decode: readA, encode: writeA) with bJson\nderive bJson = Json.codec<B>(maxBodyBytes: 64, maxDepth: 8, decode: readB, encode: writeB) with aJson", "EF138", "recursive layout through codec aJson"},
		{"nesting counts the representation", "record Wire { email: Email }\nfn readWire(wire: Wire) -> Email { wire.email }\nfn writeWire(email: Email) -> Wire { Wire { email: email } }\nderive c = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: readWire, encode: writeWire)", "EF138", "plan nesting 2 exceeds maxDepth 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Compile(header + tc.source)
			if r.Checked {
				t.Fatalf("source was admitted")
			}
			found := false
			for _, d := range r.Diagnostics {
				found = found || d.Code == tc.code && strings.Contains(d.Message, tc.message)
			}
			if !found {
				t.Fatalf("want %s %q: %+v", tc.code, tc.message, r.Diagnostics)
			}
			if _, _, err := r.Emit(false); err == nil {
				t.Fatal("refused source emitted")
			}
		})
	}
	// Positive control: the header itself checks, so each refusal above is
	// the case's own, and a composing parent checks.
	if r := Compile(header); !r.Checked {
		t.Fatalf("%+v", r.Diagnostics)
	}
	// A transform node adds no nesting of its own: Box holds Email as a
	// string, so one level suffices.
	if r := Compile(header + "derive c = Json.codec<Box>(maxBodyBytes: 64, maxDepth: 1) with emailJson"); !r.Checked {
		t.Fatalf("%+v", r.Diagnostics)
	}
}

// Transformed witnesses format on one line, `with` clause included.
func TestCodecTransformationDeclarationsFormat(t *testing.T) {
	source := "import Json \"effra/json\"\nrecord Email { text: string }\nrecord Box { email: Email }\nfn parseEmail(text: string) -> Email { Email { text: text } }\nfn renderEmail(email: Email) -> string { email.text }\nderive emailJson=Json.codec<Email>( maxBodyBytes:64,maxDepth:1,decode:parseEmail , encode : renderEmail )\nderive boxJson = Json.codec<Box>(maxBodyBytes: 64, maxDepth: 2)  with   emailJson\n"
	want := "import Json \"effra/json\"\nrecord Email {\n    text: string\n}\nrecord Box {\n    email: Email\n}\nfn parseEmail(text: string) -> Email {\n    Email {\n        text: text\n    }\n}\nfn renderEmail(email: Email) -> string {\n    email.text\n}\nderive emailJson = Json.codec<Email>(maxBodyBytes: 64, maxDepth: 1, decode: parseEmail, encode: renderEmail)\nderive boxJson = Json.codec<Box>(maxBodyBytes: 64, maxDepth: 2) with emailJson\n"
	assertFormat(t, source, want)
}
