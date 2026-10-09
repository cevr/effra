package compiler

import (
	"encoding/json"
	"fmt"
	"reflect"
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
	for _, required := range []string{`"sourceSet":true`, `"sourcePath":""`, `"unresolved":false`, `"potentialOwner":true`, `"remainder":false`, `"executed":`, `"evaluation":`, `"requiredChoice":false`, `"defaultValue":null`} {
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
					d.Occurrences[i].Ownership = append(d.Occurrences[i].Ownership, summaryOwner{Status: "unknown", OwnerKind: "callback-result", Region: summaryRegion{Kind: "unknown", Ordinal: -1}, PotentialOwner: true, Relation: summaryReference{Kind: "relation", Ref: ref}})
					return
				}
			}
		}},
		{"false potential", func(d *interfaceSummary) { d.Declarations[0].Ownership[0].PotentialOwner = false }},
		{"erased ownership", func(d *interfaceSummary) { d.Declarations[0].Ownership = []summaryOwner{} }},
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
