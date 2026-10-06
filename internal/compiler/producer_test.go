package compiler

import (
	"encoding/json"
	"strings"
	"testing"

	"effra.local/prototype/internal/producer"
)

func TestPassiveProducerQualificationPreservesSemanticIdentity(t *testing.T) {
	source := `effect fn main() -> string { "ok" }`
	r := CompileFor(source, "go")
	before := r.CheckResponse()
	if _, ok := before["producer"]; ok {
		t.Fatal("compile acquired producer identity")
	}
	goBefore, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	revision := r.Revision
	id := producer.Identity{Strength: "executing-artifact", ReuseScope: "artifact", Digest: "sha256:" + strings.Repeat("a", 64), Qualifier: "sha256:" + strings.Repeat("a", 64)}
	if err := r.Qualify(id); err != nil {
		t.Fatal(err)
	}
	after := r.CheckResponse()
	graph, err := r.Graph()
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := r.DiagnosticReport(SourceSnapshot{Text: source}, false)
	lint := r.Lint(true)
	if r.Revision != revision || r.ProducerIdentity != SemanticProducerIdentity || r.producerMetadata.Snapshot.Producer != id.Qualifier || graph.Producer != id || diagnostics.Producer != id || lint.Producer != id || after["producer"] != id {
		t.Fatal("qualification lost or rewrote checked facts")
	}
	if err := r.RequireProducer(id.Qualifier); err != nil {
		t.Fatal(err)
	}
	if r.RequireProducer("sha256:"+strings.Repeat("b", 64)) == nil {
		t.Fatal("stale producer accepted")
	}
	goAfter, err := r.EmitGo()
	if err != nil || goBefore != goAfter {
		t.Fatal("qualification changed emitted executable")
	}
	data, _ := json.Marshal(after)
	if !strings.Contains(string(data), `"snapshot"`) {
		t.Fatal(string(data))
	}
}

func TestProducerMetadataBoundsAndUnavailableReuse(t *testing.T) {
	r := Compile(`fn value() -> string { "ok" }`)
	id := producer.Identity{Strength: "unavailable", ReuseScope: "none", Reason: "image unavailable"}
	if err := r.Qualify(id); err != nil {
		t.Fatal(err)
	}
	if r.RequireProducer("process:other") == nil {
		t.Fatal("unavailable identity reused")
	}
	id.ReuseScope, id.Qualifier = "process", "process:ours"
	if err := r.Qualify(id); err != nil {
		t.Fatal(err)
	}
	if r.RequireProducer("process:ours") != nil || r.RequireProducer("process:other") == nil {
		t.Fatal("process qualification lost")
	}
	id.Declaration.Module = strings.Repeat("x", 257)
	if r.Qualify(id) == nil {
		t.Fatal("unbounded declaration accepted")
	}
}

func TestGraphProducerMetadataIsChargedAtCompatibilityBoundary(t *testing.T) {
	source := `effect fn main() -> string { "ok" }`
	plain := Compile(source)
	graph, err := plain.Graph()
	if err != nil {
		t.Fatal(err)
	}
	limits := plain.projectionLimits()
	limits.CompatibilityBytes = graph.TypeProjectionUsage.CompatibilityBytes
	plain.TypeProjectionLimits = limits
	if _, err := plain.Graph(); err != nil {
		t.Fatal("unqualified control refused", err)
	}
	id := producer.Identity{Strength: "unavailable", ReuseScope: "process", Qualifier: "process:control", Reason: "image unavailable"}
	if err := plain.Qualify(id); err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Graph(); err == nil || !strings.Contains(err.Error(), "compatibility") {
		t.Fatal("graph producer escaped compatibility budget", err)
	}
}
