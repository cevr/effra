package compiler

import (
	"encoding/hex"
	"fmt"
	"strings"

	"effra.local/prototype/internal/producer"
)

// SnapshotQualification scopes canonical references without changing source
// revisions or type IDs. ProducerIdentity remains the private compatibility ABI.
type SnapshotQualification struct {
	SchemaVersion int    `json:"schemaVersion"`
	Revision      string `json:"revision"`
	Target        string `json:"target"`
	Producer      string `json:"producer"`
	ReuseScope    string `json:"reuseScope"`
}

type ProducerMetadata struct {
	Producer producer.Identity     `json:"producer,omitzero"`
	Snapshot SnapshotQualification `json:"snapshot,omitzero"`
}

// Qualify accepts passive facts acquired by an adapter. Compile, check, emit
// and private interface caches never call producer.Current or perform I/O here.
func (r *Result) Qualify(identity producer.Identity) error {
	if err := validateProducer(identity); err != nil {
		return err
	}
	r.producerMetadata = ProducerMetadata{Producer: identity, Snapshot: SnapshotQualification{
		SchemaVersion: r.SchemaVersion, Revision: r.Revision, Target: r.Target,
		Producer: identity.Qualifier, ReuseScope: identity.ReuseScope,
	}}
	return nil
}

func validateProducer(id producer.Identity) error {
	for _, value := range []string{id.Strength, id.Digest, id.Qualifier, id.ReuseScope, id.Reason, id.Declaration.GoVersion, id.Declaration.Module, id.Declaration.Version, id.Declaration.VCSRevision, id.Declaration.VCSModified, id.Declaration.GOOS, id.Declaration.GOARCH, id.Declaration.CGOEnabled} {
		if len(value) > 256 {
			return fmt.Errorf("producer metadata exceeds its field bound")
		}
	}
	valid := false
	if id.Strength == "executing-artifact" {
		bytes, err := hex.DecodeString(strings.TrimPrefix(id.Digest, "sha256:"))
		valid = err == nil && len(bytes) == 32 && strings.HasPrefix(id.Digest, "sha256:") && id.Qualifier == id.Digest && id.ReuseScope == "artifact" && id.Reason == ""
	} else if id.Strength == "unavailable" && id.Digest == "" && id.Reason != "" {
		valid = (id.ReuseScope == "none" && id.Qualifier == "") || (id.ReuseScope == "process" && strings.HasPrefix(id.Qualifier, "process:") && len(id.Qualifier) > len("process:"))
	}
	if !valid {
		return fmt.Errorf("invalid producer qualification")
	}
	return nil
}

func (r *Result) AddProducer(response map[string]any) {
	if r.producerMetadata.Producer.Strength != "" {
		response["producer"], response["snapshot"] = r.producerMetadata.Producer, r.producerMetadata.Snapshot
	}
}

// RequireProducer is opt-in for clients reusing facts. Revision equality alone
// does not qualify cross-build reuse; an unavailable nonreusable identity fails.
func (r *Result) RequireProducer(expected string) error {
	if expected != "" && (r.producerMetadata.Producer.Qualifier == "" || expected != r.producerMetadata.Producer.Qualifier) {
		return fmt.Errorf("stale producer qualification")
	}
	return nil
}
