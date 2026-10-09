package compiler

import "encoding/json"

// The inspection view of owner evidence (lane E2 R5 design, row 37). The
// checker keeps an exact owner set per path; the public view keeps one entry
// per path with its compatibility fields (status, the canonical-first owner's
// region and origin) and adds every owner and an evidence grade. The fields
// are additive: a client reading only path, status and region sees what it
// saw before, and an empty owner list is never published as a proof.

// PublicOwnership is the owner evidence of one path.
type PublicOwnership struct {
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
	Region string `json:"region,omitempty"`
	Origin string `json:"origin,omitempty"`
	// Owners lists every owner the path may have, canonical first.
	Owners []PublicOwner `json:"owners"`
	// Evidence is "proven" when every owner was derived from a summary or a
	// construct, "conservative" when an owner was assumed for an unknown
	// producer, and "unknown" when the owner set is unbounded or deferred.
	Evidence string `json:"evidence"`
}

// PublicOwner is one owner of a path: its kind (parameter, exec, lexical,
// child, timeout, relation or unknown) and region. A parameter owner also
// publishes its selector, the path inside the parameter it came from: two
// owners of one path in the same parameter that differ only in selector are
// distinct owners, so the view must not collapse them. A nil selector is
// not parameter-relative; the empty selector is the parameter itself.
type PublicOwner struct {
	Kind     string  `json:"kind"`
	Region   string  `json:"region,omitempty"`
	Selector *string `json:"selector,omitempty"`
}

// conservativeOrigins label owners assumed for a producer the checker cannot
// see into.
var conservativeOrigins = map[string]bool{
	opaqueProducerOrigin:  true,
	operationResultOrigin: true,
	recipeResultOrigin:    true,
}

// publicOwnership groups facts by path, keeping the canonical fact order.
func publicOwnership(facts []OwnershipFact) []PublicOwnership {
	var out []PublicOwnership
	index := map[string]int{}
	for _, fact := range facts {
		i, seen := index[fact.Path]
		if !seen {
			i = len(out)
			index[fact.Path] = i
			out = append(out, PublicOwnership{Path: fact.Path, Status: "borrowed", Region: fact.Region, Origin: fact.Origin, Owners: []PublicOwner{}, Evidence: "proven"})
		}
		view := &out[i]
		owner := PublicOwner{Kind: summaryOwnerKinds[fact.ownerKind], Region: fact.Region}
		if fact.sourceSet {
			selector := fact.source
			owner.Selector = &selector
		}
		view.Owners = append(view.Owners, owner)
		switch {
		case fact.Status == "unknown" || fact.potentialOwner || fact.ownerKind == ownershipOwnerRelation:
			view.Status, view.Evidence = "unknown", "unknown"
		case view.Status != "unknown" && (fact.Status != "borrowed" || fact.ownerKind != ownershipOwnerParameter):
			view.Status = "owned"
		}
		if view.Evidence == "proven" && conservativeOrigins[fact.Origin] {
			view.Evidence = "conservative"
		}
	}
	return out
}

// MarshalJSON publishes a value's owner evidence through the per-path view.
func (v ValueType) MarshalJSON() ([]byte, error) {
	type plain ValueType
	return json.Marshal(struct {
		plain
		Ownership []PublicOwnership `json:"ownership,omitempty"`
		Captures  []PublicOwnership `json:"captures,omitempty"`
		Child     []PublicOwnership `json:"childOwnership,omitempty"`
	}{plain(v), publicOwnership(v.Ownership), publicOwnership(v.Captures), publicOwnership(v.Child)})
}
