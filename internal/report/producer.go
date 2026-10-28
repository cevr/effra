package report

import (
	"encoding/hex"
	"fmt"
	"strings"

	"ergo.local/prototype/internal/producer"
)

// SnapshotQualification scopes the facts of one result to the producer that
// computed them, without changing the source revision.
type SnapshotQualification struct {
	SchemaVersion int    `json:"schemaVersion"`
	Revision      string `json:"revision"`
	Target        string `json:"target"`
	Producer      string `json:"producer"`
	ReuseScope    string `json:"reuseScope"`
}

// ProducerMetadata is the producer identity a qualified projection carries,
// and the snapshot it qualifies.
type ProducerMetadata struct {
	Producer producer.Identity     `json:"producer,omitzero"`
	Snapshot SnapshotQualification `json:"snapshot,omitzero"`
}

// maxProducerField bounds every string of a producer identity.
const maxProducerField = 256

// Qualify attaches a producer identity the adapter acquired. Checking never
// acquires one: only an adapter that prints facts calls producer.Current and
// hands the identity here. A malformed or unbounded identity is refused.
func (r *Result) Qualify(identity producer.Identity) error {
	if err := validateProducer(identity); err != nil {
		return err
	}
	r.producer = ProducerMetadata{Producer: identity, Snapshot: SnapshotQualification{
		SchemaVersion: r.SchemaVersion, Revision: r.Revision, Target: r.Target,
		Producer: identity.Qualifier, ReuseScope: identity.ReuseScope,
	}}
	return nil
}

// validateProducer admits the two identity shapes: an executing artifact
// qualified by its own SHA-256 digest, reusable across processes, and an
// unavailable identity with a reason, reusable nowhere or only within its
// process.
func validateProducer(id producer.Identity) error {
	for _, value := range []string{id.Strength, id.Digest, id.Qualifier, id.ReuseScope, id.Reason, id.Declaration.GoVersion, id.Declaration.Module, id.Declaration.Version, id.Declaration.VCSRevision, id.Declaration.VCSModified, id.Declaration.GOOS, id.Declaration.GOARCH, id.Declaration.CGOEnabled} {
		if len(value) > maxProducerField {
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
