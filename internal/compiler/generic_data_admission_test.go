package compiler

import "testing"

func TestGenericDataBareApplicationsRequireAllArguments(t *testing.T) {
	for _, source := range []string{
		`record Box<T:type>{value:T} record Holder{box:Box}`,
		`record Box<T:type>{value:T} fn inspect(value:Box)->string{"x"}`,
		`enum Phantom<F:callable fn(A)->A,A:type>{None} fn inspect(value:Phantom)->string{"x"}`,
	} {
		r := CompileFor(source, "go")
		if r.Checked || !hasCode(r, "EF127") {
			t.Fatalf("go bare generic application must be refused with EF127: checked=%v diagnostics=%+v source=%s", r.Checked, r.Diagnostics, source)
		}
	}
	for _, source := range []string{
		`import Data "ergo/data" record Holder{option:Data.Option}`,
		`import Data "ergo/data" fn inspect(value:Data.Option)->string{"x"}`,
	} {
		r := CompileFor(source, "go")
		if r.Checked || len(r.Diagnostics) == 0 {
			t.Fatalf("go bare bundled generic owner escaped type admission: %+v source=%s", r.Diagnostics, source)
		}
	}
	if r := CompileFor(`record Box<T:type>{value:T} fn inspect(value:Box<string>)->string{value.value}`, "go"); !r.Checked {
		t.Fatalf("go complete generic application was refused: %+v", r.Diagnostics)
	}
}
