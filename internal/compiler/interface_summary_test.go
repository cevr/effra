package compiler

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestBundledTransportConcurrentCacheConsumers(t *testing.T) {
	for i := 0; i < 16; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			target := []string{"go", "js"}[i%2]
			source := []string{bundledGreeting, bundledConfiguration}[i%2]
			source = strings.ReplaceAll(source, "Fns", fmt.Sprintf("Consumer%d", i))
			r := CompileFor(source, target)
			if !r.Checked || len(r.BundledInterfaces) != 1 {
				t.Fatal(r.Diagnostics)
			}
			f := r.Program.BundledFunctions[0]
			if r.projector.admittedSummaries[f.Module].ContentHash != r.BundledInterfaces[0].InterfaceHash {
				t.Fatal("cache consumer changed transport")
			}
		})
	}
}

func TestBundledTransportUsesFreshArenasAndCompleteOperationalFacts(t *testing.T) {
	source := `import Fns "effra/functions"
effect fn keep(file: File) -> File { file }
effect fn outer(file: File) -> File { scope { run Fns.forwardFile(keep, file) } }
effect fn main() -> void { void }`
	first := Compile(source)
	second := Compile(`record Unrelated { value: i64 } ` + strings.ReplaceAll(source, "Fns", "Other"))
	if !first.Checked || !second.Checked {
		t.Fatal(first.Diagnostics, second.Diagnostics)
	}
	a, b := first.projector, second.projector
	da, db := a.admittedSummaries["effra/functions"], b.admittedSummaries["effra/functions"]
	if !reflect.DeepEqual(da, db) {
		t.Fatal("alias/order/caller changed immutable transport")
	}
	fa, fb := first.Program.BundledFunctions[0], second.Program.BundledFunctions[0]
	if fa == fb || a.values == b.values {
		t.Fatal("arena or declaration reused")
	}
	if len(fa.Ownership) == 0 || fa.Ownership[0].callbackRelation == nil || fb.Ownership[0].callbackRelation == nil || fa.Ownership[0].callbackRelation == fb.Ownership[0].callbackRelation {
		t.Fatal("callback relation was lost or reused across arenas")
	}
	if fa.Ownership[0].callbackRelation.callee.parameter != fa || fb.Ownership[0].callbackRelation.callee.parameter != fb {
		t.Fatal("parameter owner rebound outside receiving declaration")
	}
	if len(da.Relations) == 0 || len(da.Occurrences) == 0 {
		t.Fatal("operational graph absent")
	}
	if len(first.BundledInterfaces) != 1 {
		t.Fatal("interface provenance missing")
	}
	info := first.BundledInterfaces[0]
	if info.SourceInput == info.InterfaceHash || info.InterfaceHash == info.ImplementationHash || info.SourceInput == info.ImplementationHash || len(info.ImplementationHash) != 64 {
		t.Fatal("identity fields conflated", info)
	}
	if first.CheckResponse()["typeProjectionComplete"] != true {
		t.Fatal("public ownership projection incomplete")
	}
	encoded, err := json.Marshal(da)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeInterfaceSummary(encoded, da.ContentHash, da.SourceInput); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{`"sourceSet":true`, `"sourcePath":""`, `"unresolved":false`, `"potentialOwner":false`, `"ownerKind":"relation"`, `"environment":[{`, `"layer":0`, `"held":`, `"effect":`, `"remainder":false`, `"executed":`, `"evaluation":`, `"requiredChoice":false`, `"defaultValue":null`} {
		if !strings.Contains(string(encoded), required) {
			t.Fatalf("lost explicit fact %s", required)
		}
	}
}

func rehashInterfaceSummaryForTest(t *testing.T, dto interfaceSummary) (interfaceSummary, []byte) {
	t.Helper()
	dto.ContentHash = ""
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	dto.ContentHash = formatDigest(string(data))
	data, err = json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	return dto, data
}

func TestLocalAndDistributedSummarySourceVersionsStayDistinct(t *testing.T) {
	local := Compile("effect fn main() -> void { void }")
	if !local.Checked {
		t.Fatal(local.Diagnostics)
	}
	localSummary, err := exportInterfaceSummary(local.projector, currentModuleIdentity, local.Revision, local.Program.Functions)
	if err != nil {
		t.Fatal(err)
	}
	if len(localSummary.Sources) != 1 {
		t.Fatalf("local source manifest = %+v", localSummary.Sources)
	}
	localSource := localSummary.Sources[0]
	if localSource.ID != "source:user" || localSource.Module != currentModuleIdentity || localSource.Digest == "" || localSource.Version != "" {
		t.Fatalf("local source must retain its unversioned content identity: %+v", localSource)
	}
	localContent := localSummary
	localContent.ContentHash = ""
	localBytes, err := json.Marshal(localContent)
	if err != nil {
		t.Fatal(err)
	}
	if formatDigest(string(localBytes)) != localSummary.ContentHash {
		t.Fatal("local summary content hash does not cover its unversioned source")
	}

	distributed := Compile(bundledGreeting)
	if !distributed.Checked {
		t.Fatal(distributed.Diagnostics)
	}
	dto, ok := distributed.projector.admittedSummaries["effra/functions"]
	if !ok || len(dto.Sources) == 0 {
		t.Fatalf("distributed summary source manifest missing: %+v", dto.Sources)
	}
	for _, source := range dto.Sources {
		if source.ID == "source:user" || source.Module != dto.Module || source.Version != bundledInterfaceVersion {
			t.Fatalf("distributed source is not versioned compiler provenance: %+v", source)
		}
	}
	wire, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeInterfaceSummary(wire, dto.ContentHash, dto.SourceInput)
	if err != nil {
		t.Fatalf("versioned compiler-distributed summary did not round trip: %v", err)
	}
	if !reflect.DeepEqual(decoded.Sources, dto.Sources) {
		t.Fatalf("distributed source manifest changed during round trip: got %+v, want %+v", decoded.Sources, dto.Sources)
	}

	missingSourceVersion := dto
	missingSourceVersion.Sources = append([]SourceInfo{}, dto.Sources...)
	missingSourceVersion.Sources[0].Version = ""
	missingSourceVersion, wire = rehashInterfaceSummaryForTest(t, missingSourceVersion)
	if _, err := decodeInterfaceSummary(wire, missingSourceVersion.ContentHash, missingSourceVersion.SourceInput); err == nil || !strings.Contains(err.Error(), "required interface field version missing") {
		t.Fatalf("missing distributed source version did not fail strict decoding: %v", err)
	}

	localProvenance := dto
	localProvenance.Sources = append([]SourceInfo{}, dto.Sources...)
	localProvenance.Sources[0].ID = "source:user"
	localProvenance.Sources[0].Module = currentModuleIdentity
	localProvenance.Sources[0].Version = bundledInterfaceVersion
	localProvenance, wire = rehashInterfaceSummaryForTest(t, localProvenance)
	decodedLocalProvenance, err := decodeInterfaceSummary(wire, localProvenance.ContentHash, localProvenance.SourceInput)
	if err != nil {
		t.Fatalf("provenance control should reach receiver manifest validation: %v", err)
	}
	if err := distributed.projector.admitInterfaceSummary(decodedLocalProvenance, distributed.Program.BundledFunctions); err == nil {
		t.Fatal("local unversioned provenance admitted as a distributed summary")
	}

	missingInterfaceVersion := dto
	missingInterfaceVersion.Version = ""
	missingInterfaceVersion, wire = rehashInterfaceSummaryForTest(t, missingInterfaceVersion)
	if _, err := decodeInterfaceSummary(wire, missingInterfaceVersion.ContentHash, missingInterfaceVersion.SourceInput); err == nil {
		t.Fatal("missing distributed interface version admitted")
	}
}

func TestBundledManagedCallbackControlsAcrossOrders(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		for _, alias := range []string{"Fns", "Other"} {
			for _, acquired := range []bool{false, true} {
				body := `file`
				if acquired {
					body = `run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`
				}
				callback := `effect fn chosen(file:File)->File raises {IoError}{` + body + `}`
				caller := `effect fn relay(file:File)->File raises {IoError}{run ` + alias + `.forwardFile(chosen,file)}
effect fn outer(file:File)->File raises {IoError}{scope {run relay(file)}}`
				for _, reverse := range []bool{false, true} {
					source := `import ` + alias + ` "effra/functions" ` + callback + caller
					if reverse {
						source = `import ` + alias + ` "effra/functions" ` + caller + callback
					}
					source += ` effect fn main()->void{void}`
					r := CompileFor(source, target)
					// Files is a native-only builtin. The borrowed case is portable;
					// the acquired case is an ownership control on the native target.
					if acquired && target == "js" {
						continue
					}
					if r.Checked == acquired || (acquired && !hasCode(r, "EF123")) {
						t.Fatalf("%s/%s/acquired=%t/reverse=%t: %+v", target, alias, acquired, reverse, r.Diagnostics)
					}
				}
			}
		}
	}
}

func TestBundledTransportRejectsMalformedAndStaleData(t *testing.T) {
	r := Compile(bundledGreeting)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	dto := r.projector.admittedSummaries["effra/functions"]
	data, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	for name, wire := range map[string][]byte{
		"trailing":                    append(append([]byte{}, data...), []byte(` {}`)...),
		"duplicate":                   []byte(strings.Replace(string(data), fmt.Sprintf(`"interfaceSchema":%d`, interfaceSummarySchema), fmt.Sprintf(`"interfaceSchema":%d,"interfaceSchema":%d`, interfaceSummarySchema, interfaceSummarySchema), 1)),
		"missing false":               []byte(strings.Replace(string(data), `,"unresolved":false`, ``, 1)),
		"missing parameter role":      []byte(strings.Replace(string(data), `"requiredChoice":false,`, ``, 1)),
		"missing parameter default":   []byte(strings.Replace(string(data), `,"defaultValue":null`, ``, 1)),
		"malformed parameter role":    []byte(strings.Replace(string(data), `"requiredChoice":false`, `"requiredChoice":0`, 1)),
		"malformed parameter default": []byte(strings.Replace(string(data), `"defaultValue":null`, `"defaultValue":false`, 1)),
		"null":                        []byte(strings.Replace(string(data), `"trustedHost":[]`, `"trustedHost":null`, 1)),
		"unknown":                     []byte(strings.Replace(string(data), `"trustedHost":[]`, `"trustedHost":[],"extra":false`, 1)),
		"stale ABI":                   []byte(strings.ReplaceAll(string(data), SemanticProducerIdentity, "stale")),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeInterfaceSummary(wire, dto.ContentHash, dto.SourceInput); err == nil {
				t.Fatal("malformed transport admitted")
			}
		})
	}
	if _, err := decodeInterfaceSummary(data, "stale", dto.SourceInput); err == nil {
		t.Fatal("stale digest admitted")
	}
	if _, err := decodeInterfaceSummary(data, dto.ContentHash, "stale"); err == nil {
		t.Fatal("stale implementation input admitted")
	}
}

// TestBundledTransportRejectsPreRecoveryProducers pins the producer epoch that
// introduced layer-indexed owners and failure payload evidence. A summary from
// the previous producer (checker ABI 9, bundled interface 4, interface schema
// 4, ownership schema 3) is refused even when its digest is self-consistent,
// so its ownership facts are never read under the new owner vocabulary.
func TestBundledTransportRejectsPreRecoveryProducers(t *testing.T) {
	r := Compile(bundledGreeting)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if SemanticProducerIdentity != "effra/checker-abi-10/bundled-interface-5" || interfaceSummarySchema != 5 || ownershipSummarySchema != 4 {
		t.Fatalf("producer epoch = %s interface=%d ownership=%d", SemanticProducerIdentity, interfaceSummarySchema, ownershipSummarySchema)
	}
	current, encoded := rehashInterfaceSummaryForTest(t, r.projector.admittedSummaries["effra/functions"])
	if _, err := decodeInterfaceSummary(encoded, current.ContentHash, current.SourceInput); err != nil {
		t.Fatalf("current producer summary refused: %v", err)
	}
	const previous = "effra/checker-abi-9/bundled-interface-4"
	for name, edit := range map[string]func(*interfaceSummary){
		"previous producer":         func(s *interfaceSummary) { s.SemanticABI, s.Producer = previous, previous },
		"previous interface schema": func(s *interfaceSummary) { s.InterfaceSchema = 4 },
		"previous ownership schema": func(s *interfaceSummary) { s.OwnershipSchema = 3 },
		"previous epoch": func(s *interfaceSummary) {
			s.SemanticABI, s.Producer, s.InterfaceSchema, s.OwnershipSchema = previous, previous, 4, 3
		},
	} {
		t.Run(name, func(t *testing.T) {
			stale := current
			edit(&stale)
			stale, wire := rehashInterfaceSummaryForTest(t, stale)
			if _, err := decodeInterfaceSummary(wire, stale.ContentHash, stale.SourceInput); err == nil || err.Error() != "incompatible interface producer or schema" {
				t.Fatalf("pre-recovery summary was not refused as incompatible: %v", err)
			}
		})
	}
}

func TestResolvedDefaultWireRoundTripUsesVersionedProducerSummary(t *testing.T) {
	r := Compile(bundledGreeting)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	dto, ok := r.projector.admittedSummaries["effra/functions"]
	if !ok || len(dto.Sources) == 0 {
		t.Fatalf("versioned producer summary missing: %+v", dto.Sources)
	}
	versionedDefault := dto
	versionedDefault.Declarations = append([]summaryDeclaration{}, dto.Declarations...)
	foundParameter := false
	for i := range versionedDefault.Declarations {
		parameters := versionedDefault.Declarations[i].Parameters
		if len(parameters) == 0 {
			continue
		}
		versionedDefault.Declarations[i].Parameters = append([]summaryParameter{}, parameters...)
		value := ConstantValue{Kind: "string", Value: "fixture"}
		versionedDefault.Declarations[i].Parameters[0].DefaultValue = &value
		foundParameter = true
		break
	}
	if !foundParameter {
		t.Fatal("versioned producer fixture has no parameter for the default wire")
	}
	versionedDefault, encoded := rehashInterfaceSummaryForTest(t, versionedDefault)
	decoded, err := decodeInterfaceSummary(encoded, versionedDefault.ContentHash, versionedDefault.SourceInput)
	if err != nil {
		t.Fatalf("versioned default-bearing producer summary did not round trip: %v", err)
	}
	foundDefault := false
	for _, declaration := range decoded.Declarations {
		for _, parameter := range declaration.Parameters {
			if parameter.DefaultValue != nil && *parameter.DefaultValue == (ConstantValue{Kind: "string", Value: "fixture"}) {
				foundDefault = true
			}
		}
	}
	if !foundDefault {
		t.Fatal("versioned summary round trip lost its resolved scalar default")
	}
	missingValue := strings.Replace(string(encoded), `"defaultValue":{"kind":"string","value":"fixture"}`, `"defaultValue":{"kind":"string"}`, 1)
	if missingValue == string(encoded) {
		t.Fatal("versioned default wire did not contain the scalar value")
	}
	if _, err := decodeInterfaceSummary([]byte(missingValue), versionedDefault.ContentHash, versionedDefault.SourceInput); err == nil || !strings.Contains(err.Error(), "required interface field value missing") {
		t.Fatalf("missing scalar value was not the causal decoder refusal: %v", err)
	}
}

func TestBundledTransportRejectsOwnerAndGraphCorruption(t *testing.T) {
	source := `import Fns "effra/functions" effect fn keep(file:File)->File{file} effect fn outer(file:File)->File{run Fns.forwardFile(keep,file)} effect fn main()->void{void}`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	dto := r.projector.admittedSummaries["effra/functions"]
	for _, test := range []struct {
		name   string
		mutate func(*interfaceSummary)
	}{
		{"parameter required-choice role", func(d *interfaceSummary) {
			if len(d.Declarations) == 0 || len(d.Declarations[0].Parameters) == 0 {
				t.Fatal("summary fixture has no parameter owner")
			}
			d.Declarations[0].Parameters[0].RequiredChoice = true
		}},
		{"parameter resolved default", func(d *interfaceSummary) {
			if len(d.Declarations) == 0 || len(d.Declarations[0].Parameters) == 0 {
				t.Fatal("summary fixture has no parameter owner")
			}
			d.Declarations[0].Parameters[0].DefaultValue = &ConstantValue{Kind: "string", Value: "stale"}
		}},
		{"declaration", func(d *interfaceSummary) { d.Declarations[0].Ref = "function:caller:module:keep" }},
		{"ordinal", func(d *interfaceSummary) {
			for i := range d.Evidence {
				if d.Evidence[i].Parameter.Kind == "parameter" {
					d.Evidence[i].Parameter.Ordinal = 99
					return
				}
			}
			t.Fatal("no parameter evidence")
		}},
		{"native nominal", func(d *interfaceSummary) {
			for i := range d.Types {
				if d.Types[i].Name == "File" {
					d.Types[i].Declaration = "native:caller:File"
					return
				}
			}
			t.Fatal("no File type")
		}},
		{"dangling occurrence", func(d *interfaceSummary) { d.Relations[0].Arguments[0] = "missing" }},
		{"cyclic relation", func(d *interfaceSummary) {
			ref := d.Relations[0].Ref
			arg := d.Relations[0].Arguments[0]
			for i := range d.Occurrences {
				if d.Occurrences[i].Ref == arg {
					env := []summaryEnvironmentOwner{{OwnerKind: "exec", Region: summaryRegion{Kind: "exec", Ordinal: 0}}, {OwnerKind: "exec", Region: summaryRegion{Kind: "exec", Ordinal: 1}}}
					d.Occurrences[i].Ownership = append(d.Occurrences[i].Ownership, summaryOwner{Status: "unknown", OwnerKind: "relation", Region: summaryRegion{Kind: "unknown", Ordinal: -1}, Relation: summaryReference{Kind: "relation", Ref: ref}, Environment: env})
					return
				}
			}
		}},
		{"erased relation environment", func(d *interfaceSummary) {
			for i := range d.Declarations {
				for j := range d.Declarations[i].Ownership {
					if d.Declarations[i].Ownership[j].OwnerKind == "relation" {
						d.Declarations[i].Ownership[j].Environment = []summaryEnvironmentOwner{}
						return
					}
				}
			}
			t.Fatal("no relation owner")
		}},
		{"free layer executor", func(d *interfaceSummary) {
			for i := range d.Declarations {
				for j := range d.Declarations[i].Ownership {
					if d.Declarations[i].Ownership[j].OwnerKind == "relation" {
						d.Declarations[i].Ownership[j].OwnerKind = "exec"
						d.Declarations[i].Ownership[j].Region = summaryRegion{Kind: "exec", Ordinal: maxSummaryLayers}
						d.Declarations[i].Ownership[j].Relation = summaryReference{Kind: "absent"}
						d.Declarations[i].Ownership[j].Environment = []summaryEnvironmentOwner{}
						return
					}
				}
			}
			t.Fatal("no relation owner")
		}},
		{"closed owner", func(d *interfaceSummary) {
			d.Declarations[0].Ownership[0].OwnerKind = "timeout"
		}},
		{"erased ownership", func(d *interfaceSummary) { d.Declarations[0].Ownership = []summaryOwner{} }},
		// Payload evidence is recomputed from the retained body: a forged
		// proof, a transient owner or a second encoding is refused.
		{"forged failure evidence", func(d *interfaceSummary) {
			d.Declarations[0].Failures = append(d.Declarations[0].Failures, summaryFailure{Layer: 0, Label: "Forged", Owners: []summaryOwner{}})
		}},
		{"closed payload owner", func(d *interfaceSummary) {
			d.Declarations[0].Failures = []summaryFailure{{Layer: 0, Label: "Forged", Owners: []summaryOwner{{Path: "file", Status: "owned", OwnerKind: "scope", Region: summaryRegion{Kind: "unknown", Ordinal: -1}, Relation: summaryReference{Kind: "absent"}, Environment: []summaryEnvironmentOwner{}}}}}
		}},
		// Failure decoder invariants and pending child evidence are covered
		// causally by TestSummaryFailureEvidenceRefusalsAreCausal.
		{"missing type", func(d *interfaceSummary) { d.Types = d.Types[1:] }},
		{"duplicate type", func(d *interfaceSummary) { d.Types = append(d.Types, d.Types[0]) }},
		{"empty nonempty row", func(d *interfaceSummary) { d.Rows[0].Labels = []string{} }},
		{"region owner", func(d *interfaceSummary) {
			d.Declarations[0].Ownership[0].Region = summaryRegion{Kind: "parameter", Declaration: "caller", Ordinal: 0}
		}},
		{"transient region", func(d *interfaceSummary) { d.Declarations[0].Ownership[0].Region.Kind = "scope" }},
		{"table budget", func(d *interfaceSummary) {
			for len(d.Types) <= maxInterfaceTableEntries {
				d.Types = append(d.Types, d.Types[0])
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			wire, _ := json.Marshal(dto)
			var corrupt interfaceSummary
			if err := json.Unmarshal(wire, &corrupt); err != nil {
				t.Fatal(err)
			}
			test.mutate(&corrupt)
			fresh := Compile(source)
			if err := fresh.projector.admitInterfaceSummary(corrupt, fresh.Program.BundledFunctions); err == nil {
				t.Fatal("corrupt summary admitted")
			}
		})
	}
}

// summaryFailureFixture checks one declaration whose genuine failure
// evidence has two ordinary labels and a pending child failure with a fork
// instance and the ordinary failures it may be raised with.
const summaryFailureFixture = `error Boom { code: i64 }
error Nope { code: i64 }
effect fn job() -> string raises { Boom } {
    fail Boom { code: 7 }
}
effect fn reject() -> void raises { Nope } {
    fail Nope { code: 2 }
}
effect fn sequel() -> string raises { Boom, Nope } uses { Scheduler } {
    let a = fork job()
    run reject()
    run a.join()
}
effect fn main() -> void { void }`

// summaryFailureFunctions is the fixture's transported declaration. The
// helpers' bodies end in fail, whose never type is a body-only type the
// summary has no owner for, so only sequel is transported.
func summaryFailureFunctions(r *Result) []*Function {
	return slices.DeleteFunc(slices.Clone(r.Program.Functions), func(f *Function) bool { return f.Name != "sequel" })
}

// TestSummaryFailureEvidenceRefusalsAreCausal mutates exactly one invariant
// of otherwise valid failure evidence and requires that invariant's own
// refusal. Each decoder check is therefore the cause of its refusal: without
// it the mutation is admitted or refused by a different check. Forged and
// erased entries are well formed and are refused only by the comparison
// with the retained body.
func TestSummaryFailureEvidenceRefusalsAreCausal(t *testing.T) {
	producer := Compile(summaryFailureFixture)
	if !producer.Checked {
		t.Fatal(producer.Diagnostics)
	}
	dto, err := exportInterfaceSummary(producer.projector, currentModuleIdentity, "failure-evidence", summaryFailureFunctions(producer))
	if err != nil {
		t.Fatal(err)
	}
	declaration := slices.IndexFunc(dto.Declarations, func(d summaryDeclaration) bool { return strings.HasSuffix(d.Ref, ":sequel") })
	if declaration < 0 {
		t.Fatal("fixture declaration missing")
	}
	wantShape := []summaryFailure{
		{Layer: 0, Label: "Boom", With: []string{}, Owners: []summaryOwner{}},
		{Layer: 0, Label: "Nope", With: []string{}, Owners: []summaryOwner{}},
		{Layer: 0, Label: "Boom", Pending: true, Fork: "#0", With: []string{"Boom", "Nope"}, Owners: []summaryOwner{}},
	}
	if got := dto.Declarations[declaration].Failures; !reflect.DeepEqual(got, wantShape) {
		t.Fatalf("fixture failure evidence changed: %+v", got)
	}
	receiver := Compile(summaryFailureFixture)
	if err := receiver.projector.admitInterfaceSummary(dto, summaryFailureFunctions(receiver)); err != nil {
		t.Fatalf("valid failure evidence refused: %v", err)
	}
	wire, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, want string
		mutate     func([]summaryFailure) []summaryFailure
	}{
		{"layer out of range", "failure evidence layer out of range", func(f []summaryFailure) []summaryFailure {
			f[0].Layer = maxSummaryLayers + 1
			return f
		}},
		{"missing label", "failure evidence without a label", func(f []summaryFailure) []summaryFailure {
			f[0].Label = ""
			return f
		}},
		{"fork without pending", "fork instance on non-pending failure evidence", func(f []summaryFailure) []summaryFailure {
			f[2].Pending = false
			return f
		}},
		{"with without pending", "with labels on non-pending failure evidence", func(f []summaryFailure) []summaryFailure {
			f[2].Pending, f[2].Fork = false, ""
			return f
		}},
		{"unsorted with", "noncanonical failure evidence with labels", func(f []summaryFailure) []summaryFailure {
			f[2].With = []string{"Nope", "Boom"}
			return f
		}},
		{"empty with label", "noncanonical failure evidence with labels", func(f []summaryFailure) []summaryFailure {
			f[2].With = []string{"Boom", "Nope", ""}
			return f
		}},
		{"out of canonical order", "failure evidence out of canonical order", func(f []summaryFailure) []summaryFailure {
			f[0], f[1] = f[1], f[0]
			return f
		}},
		{"forged pending failure", "failures=false", func(f []summaryFailure) []summaryFailure {
			return append(f, summaryFailure{Layer: 0, Label: "Nope", Pending: true, Fork: "#9", With: []string{}, Owners: []summaryOwner{}})
		}},
		{"erased failure", "failures=false", func(f []summaryFailure) []summaryFailure {
			return slices.Delete(f, 1, 2)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var corrupt interfaceSummary
			if err := json.Unmarshal(wire, &corrupt); err != nil {
				t.Fatal(err)
			}
			corrupt.Declarations[declaration].Failures = test.mutate(corrupt.Declarations[declaration].Failures)
			fresh := Compile(summaryFailureFixture)
			err := fresh.projector.admitInterfaceSummary(corrupt, summaryFailureFunctions(fresh))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("refusal = %v, want %q", err, test.want)
			}
		})
	}
}

// A pending failure keeps the ordinary failures it may be raised with
// across a function edge (design §5.3 rule 6): the encoding is canonical and
// round-trips.
func TestPendingFailureWithRoundTrips(t *testing.T) {
	c := Compile("effect fn main() -> void { void }").projector
	keys := []failureKey{
		{layer: 0, label: "A", pending: true, fork: "#0"},
		{layer: 0, label: "A", pending: true, fork: "#0", with: "B,C"},
		{layer: 0, label: "A", pending: true, fork: "#1", with: "B"},
	}
	evidence := failureEvidence{}
	for _, key := range keys {
		evidence[key] = nil
	}
	x := &summaryExporter{}
	encoded := x.failures(nil, evidence, 0)
	for i, item := range encoded {
		if !canonicalWith(item.With) {
			t.Fatalf("item %d: with %v is not canonical", i, item.With)
		}
	}
	a := &summaryAdmission{c: c}
	decoded := a.failures(encoded, 0)
	if a.err != nil {
		t.Fatal(a.err)
	}
	if !equalFailures(decoded, evidence) {
		t.Fatalf("round trip changed the evidence: %v != %v", decoded, evidence)
	}
	for _, with := range [][]string{{"B", "A"}, {"A", "A"}, {""}, {"A,B"}} {
		if canonicalWith(with) {
			t.Fatalf("with %q is not canonical", with)
		}
	}
}
