package compiler

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// A path with a borrowed and an acquired owner publishes one entry: owned,
// the canonical-first owner's region for compatibility, both owners, and a
// conservative grade because the acquisition is an operation result.
func TestPublicOwnershipViewListsEveryOwner(t *testing.T) {
	r := Compile(`effect fn pick(flag: bool, file: File) -> File raises { IoError } uses { Files } {
    if flag { file } else { run Files.openRead("x").provide<Files>(LiveFiles) }
}
effect fn main() -> void {
    void
}`)
	symbol := r.Find("pick")
	if !r.Checked || symbol == nil {
		t.Fatal(r.Diagnostics)
	}
	encoded, err := json.Marshal(symbol.Contract)
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		Ownership []PublicOwnership `json:"ownership"`
	}
	if err := json.Unmarshal(encoded, &view); err != nil {
		t.Fatal(err)
	}
	empty := ""
	want := []PublicOwnership{{Status: "owned", Region: "parameter:file", Origin: "parameter", Owners: []PublicOwner{{Kind: "parameter", Region: "parameter:file", Selector: &empty}, {Kind: "exec", Region: "exec:0"}}, Evidence: "conservative"}}
	if !reflect.DeepEqual(view.Ownership, want) {
		t.Fatalf("public owner view %+v, want %+v", view.Ownership, want)
	}
}

// A deferred or unbounded owner is never published as a proof.
func TestPublicOwnershipViewGradesUnknownOwners(t *testing.T) {
	facts := []OwnershipFact{
		{Path: "a", Status: "borrowed", Region: "parameter:p", ownerKind: ownershipOwnerParameter},
		{Path: "b", Status: "unknown", ownerKind: ownershipOwnerRelation},
		{Path: "b", Status: "borrowed", Region: "parameter:p", ownerKind: ownershipOwnerParameter},
		{Path: "c", Status: "owned", Region: "scope:4", ownerKind: ownershipOwnerLexical, potentialOwner: true},
	}
	got := publicOwnership(facts)
	if len(got) != 3 || got[0].Status != "borrowed" || got[0].Evidence != "proven" || got[1].Status != "unknown" || got[1].Evidence != "unknown" || len(got[1].Owners) != 2 || got[2].Status != "unknown" || got[2].Evidence != "unknown" {
		t.Fatalf("public owner view %+v", got)
	}
}

// Two parameter owners of one path which differ only in their selector are
// two owners in the public JSON, as they are in the checker (ownerIdentity):
// the view publishes the selector, so a client can tell them apart.
func TestPublicOwnerKeepsParameterSelectors(t *testing.T) {
	r := Compile(`record Pair { outer: File, inner: File }
fn choose(flag: bool, pair: Pair) -> File { if flag { pair.outer } else { pair.inner } }
effect fn main() -> void {
    void
}`)
	symbol := r.Find("choose")
	if !r.Checked || symbol == nil {
		t.Fatal(r.Diagnostics)
	}
	encoded, err := json.Marshal(symbol.Contract)
	if err != nil {
		t.Fatal(err)
	}
	var view struct {
		Ownership []struct {
			Owners []map[string]any `json:"owners"`
		} `json:"ownership"`
	}
	if err := json.Unmarshal(encoded, &view); err != nil {
		t.Fatal(err)
	}
	var selectors []string
	for _, entry := range view.Ownership {
		for _, owner := range entry.Owners {
			selector, ok := owner["selector"].(string)
			if owner["kind"] == "parameter" && !ok {
				t.Fatalf("parameter owner without a selector: %s", encoded)
			}
			selectors = append(selectors, selector)
		}
	}
	sort.Strings(selectors)
	if !reflect.DeepEqual(selectors, []string{"inner", "outer"}) {
		t.Fatalf("want the two selectors inner and outer, got %v in %s", selectors, encoded)
	}
}
