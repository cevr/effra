package compiler

import "testing"

func TestGenericDataBareApplicationsRequireAllArguments(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		for _, source := range []string{
			`record Box<T:type>{value:T} record Holder{box:Box}`,
			`record Box<T:type>{value:T} fn inspect(value:Box)->string{"x"}`,
			`enum Phantom<F:callable fn(A)->A,A:type>{None} fn inspect(value:Phantom)->string{"x"}`,
		} {
			r := CompileFor(source, target)
			if r.Checked || !hasCode(r, "EF127") {
				t.Fatalf("%s bare generic application must be refused with EF127: checked=%v diagnostics=%+v source=%s", target, r.Checked, r.Diagnostics, source)
			}
		}
		for _, source := range []string{
			`import Data "effra/data" record Holder{option:Data.Option}`,
			`import Data "effra/data" fn inspect(value:Data.Option)->string{"x"}`,
		} {
			r := CompileFor(source, target)
			if r.Checked || len(r.Diagnostics) == 0 {
				t.Fatalf("%s bare bundled generic owner escaped type admission: %+v source=%s", target, r.Diagnostics, source)
			}
		}
		if r := CompileFor(`record Box<T:type>{value:T} fn inspect(value:Box<string>)->string{value.value}`, target); !r.Checked {
			t.Fatalf("%s complete generic application was refused: %+v", target, r.Diagnostics)
		}
	}
}
