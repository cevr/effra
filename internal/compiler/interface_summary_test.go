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
effect fn main() -> () { () }`
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
	for _, required := range []string{`"sourceSet":true`, `"sourcePath":""`, `"unresolved":false`, `"potentialOwner":true`, `"remainder":false`, `"executed":`, `"evaluation":`} {
		if !strings.Contains(string(encoded), required) {
			t.Fatalf("lost explicit fact %s", required)
		}
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
					source += ` effect fn main()->(){()}`
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
		"trailing":      append(append([]byte{}, data...), []byte(` {}`)...),
		"duplicate":     []byte(strings.Replace(string(data), `"interfaceSchema":1`, `"interfaceSchema":1,"interfaceSchema":1`, 1)),
		"missing false": []byte(strings.Replace(string(data), `,"unresolved":false`, ``, 1)),
		"null":          []byte(strings.Replace(string(data), `"trustedHost":[]`, `"trustedHost":null`, 1)),
		"unknown":       []byte(strings.Replace(string(data), `"trustedHost":[]`, `"trustedHost":[],"extra":false`, 1)),
		"stale ABI":     []byte(strings.ReplaceAll(string(data), SemanticProducerIdentity, "stale")),
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

func TestBundledTransportRejectsOwnerAndGraphCorruption(t *testing.T) {
	source := `import Fns "effra/functions" effect fn keep(file:File)->File{file} effect fn outer(file:File)->File{run Fns.forwardFile(keep,file)} effect fn main()->(){()}`
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	dto := r.projector.admittedSummaries["effra/functions"]
	for _, test := range []struct {
		name   string
		mutate func(*interfaceSummary)
	}{
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
