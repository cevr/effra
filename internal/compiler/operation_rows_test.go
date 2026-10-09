package compiler

import (
	"strings"
	"testing"
)

// Row parameters on service operations and implementation methods. Rows are
// erased, so an operation may be row-polymorphic where it cannot be
// type-polymorphic (Go struct fields cannot be generic).

const operationRowPrelude = `record Req {
    path: string,
}
service Server {
    effect fn listen<E: raises, R: uses>(handler: effect fn(Req) -> string raises { E } uses { R }) -> void raises { E } uses { R }
}
`

const operationRowsRunnable = operationRowPrelude + `impl Fake for Server {
    effect fn listen<A: raises, B: uses>(handler: effect fn(Req) -> string raises { A } uses { B }) -> void raises { A } uses { B } {
        let out = run handler(Req { path: "/x" })
        void
    }
}
effect fn route(r: Req) -> string uses { Console } {
    run Console.log(r.path)
    r.path
}
effect fn main() -> void {
    run Server.listen(route).provide<Server>(Fake).provide<Console>(Stdout)
}
`

func TestOperationRowParametersCheckAcrossTargets(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		r := CompileFor(operationRowsRunnable, target)
		if !r.Checked {
			t.Fatalf("%s: operation row parameters refused: %+v", target, r.Diagnostics)
		}
		for _, d := range r.Diagnostics {
			t.Fatalf("%s: unexpected diagnostic %+v", target, d)
		}
	}
}

// cb2: a service operation whose signature is the general callback form with
// row parameters on both the callback and the operation's own `uses`.
func TestOperationRowParametersAdmitTheGeneralCallbackForm(t *testing.T) {
	source := `record Req {
    path: string,
}
service Server {
    effect fn listen<E: raises, R: uses>(handler: effect fn(Req) -> string raises { E } uses { R }) -> void uses { R }
}
effect fn main() -> void {
    void
}
`
	for _, target := range []string{"go", "js"} {
		if r := CompileFor(source, target); !r.Checked {
			t.Fatalf("%s: %+v", target, r.Diagnostics)
		}
	}
}

// cb3: a callback with a service row passes to an operation with a closed
// provider; the caller's row is the operation service plus the callback row.
func TestOperationRowsInstantiateFromTheCallback(t *testing.T) {
	source := operationRowsRunnable
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	info, err := r.TypeAt(strings.Index(source, `listen(route)`))
	if err != nil {
		t.Fatal(err)
	}
	if info.Type.Application == nil || len(info.Type.Application.RowArguments) != 2 {
		t.Fatalf("operation row arguments missing: %+v", info.Type.Application)
	}
	for _, argument := range info.Type.Application.RowArguments {
		if argument.Parameter.Declaration == "" || argument.Parameter.Kind == "" {
			t.Fatalf("row argument lost its parameter: %+v", argument)
		}
	}
	if !contains(info.Type.Services, "Server") || !contains(info.Type.Services, "Console") {
		t.Fatalf("operation call lost the service or callback row: %+v", info.Type)
	}
	if len(info.ExecutedFailures) > 0 || len(info.ExecutedRequirements) > 0 {
		t.Fatal("constructing an operation recipe executed it")
	}
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func TestOperationRowParametersDeclareGenericTypeScript(t *testing.T) {
	r := CompileFor(operationRowsRunnable, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	_, declaration, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`export type ServerProvider = { readonly "listen": <__ef_row_0_E extends { readonly _tag: string }, __ef_row_0_R>(arg_handler: (arg0: Req) => Effect.Effect<string, __ef_row_0_E, __ef_row_0_R>) => Effect.Effect<void, __ef_row_0_E, __ef_row_0_R>; };`,
		// The impl keeps the operation's rows; only construction services are hidden.
		`declare const __ef_provider_Fake: { readonly "listen": <__ef_row_0_A extends { readonly _tag: string }, __ef_row_0_B>(arg_handler: (arg0: Req) => Effect.Effect<string, __ef_row_0_A, __ef_row_0_B>) => Effect.Effect<void, __ef_row_0_A, __ef_row_0_B>; };`,
	} {
		if !strings.Contains(declaration, want) {
			t.Fatalf("declaration lost the operation rows, want %s in:\n%s", want, declaration)
		}
	}
	t.Run("strict TypeScript consumer", func(t *testing.T) {
		checkStrictTypeScript(t, declaration, `import { Fake, Server } from "./generated.mjs";
import type { Req } from "./generated.mjs";
import type { Effect } from "effect";
declare const provider: typeof Fake;
declare const route: (r: Req) => Effect.Effect<string, { readonly _tag: "Boom" }, never>;
const run: Effect.Effect<void, { readonly _tag: "Boom" }, never> = provider.listen(route);
void run; void Server;`)
	})
	// A non-empty R must flow through the impl type. `keeps` rejects widening;
	// the expected error rejects collapsing R to never.
	t.Run("strict TypeScript consumer keeps a non-empty requirement", func(t *testing.T) {
		checkStrictTypeScript(t, declaration, `import { Fake } from "./generated.mjs";
import type { Req } from "./generated.mjs";
import type { Effect } from "effect";
interface Needs { readonly _needs: "needs" }
declare const provider: typeof Fake;
declare const route: (r: Req) => Effect.Effect<string, never, Needs>;
const run = provider.listen(route);
const keeps: Effect.Effect<void, never, Needs> = run;
// @ts-expect-error R must not collapse to never
const collapsed: Effect.Effect<void, never, never> = run;
void keeps; void collapsed;`)
	})
}

func TestOperationRowParametersFormatterRoundTrip(t *testing.T) {
	formatted, err := FormatSource(operationRowsRunnable)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		`effect fn listen<E: raises, R: uses>(handler: effect fn(Req) -> string raises { E } uses { R }) -> void raises { E } uses { R }`,
		`effect fn listen<A: raises, B: uses>(handler: effect fn(Req) -> string raises { A } uses { B }) -> void raises { A } uses { B } {`,
	} {
		if !strings.Contains(formatted.Text, line) {
			t.Fatalf("formatter changed an operation signature: %q\n%s", line, formatted.Text)
		}
	}
	again, err := FormatSource(formatted.Text)
	if err != nil || again.Text != formatted.Text {
		t.Fatalf("formatter is not idempotent: %v\n%s\n----\n%s", err, formatted.Text, again.Text)
	}
	if r := Compile(formatted.Text); !r.Checked {
		t.Fatalf("formatting changed admission: %+v", r.Diagnostics)
	}
}

func diagnosticCodes(r *Result) []string {
	codes := []string{}
	for _, d := range r.Diagnostics {
		codes = append(codes, d.Code)
	}
	return codes
}

func TestOperationRowParameterNegativeControls(t *testing.T) {
	impl := func(header, body string) string {
		return operationRowPrelude + `impl Fake for Server {
    ` + header + ` {
` + body + `
    }
}
effect fn main() -> void {
    void
}
`
	}
	const run = `        let out = run handler(Req { path: "/x" })
        void`
	good := `effect fn listen<A: raises, B: uses>(handler: effect fn(Req) -> string raises { A } uses { B }) -> void raises { A } uses { B }`
	for _, test := range []struct {
		name   string
		source string
		want   string
	}{
		// cb3 as the spike wrote it: a closed-row operation must not accept a
		// callback that uses a service. Row parameters are the opt-in.
		{"closed operation rejects a callback with rows", `record Req {
    path: string,
}
service Server {
    effect fn listen(handler: effect fn(Req) -> string) -> void
}
effect fn route(r: Req) -> string uses { Console } {
    run Console.log(r.path)
    r.path
}
effect fn main() -> void {
    run Server.listen(route)
}
`, "EF106"},
		{"type parameter on an operation", `service S {
    effect fn put<T: type>(value: T) -> void
}
effect fn main() -> void {
    void
}
`, "EF125"},
		{"type parameter on an implementation method", `service S {
    effect fn put(value: string) -> void
}
impl Memory for S {
    effect fn put<T: type>(value: string) -> void {
        void
    }
}
effect fn main() -> void {
    void
}
`, "EF125"},
		{"fixed label in an operation uses", `service S {
    effect fn put() -> void uses { Console }
}
effect fn main() -> void {
    void
}
`, "EF103"},
		{"fixed label beside a row parameter", `service S {
    effect fn put<R: uses>(cb: effect fn() -> void uses { R }) -> void uses { R, Console }
}
effect fn main() -> void {
    void
}
`, "EF103"},
		{"implementation row arity", impl(`effect fn listen<A: raises>(handler: effect fn(Req) -> string raises { A }) -> void raises { A }`, run), "EF104"},
		{"implementation without row parameters", impl(`effect fn listen(handler: effect fn(Req) -> string) -> void`, run), "EF104"},
		{"implementation row kind order", impl(`effect fn listen<B: uses, A: raises>(handler: effect fn(Req) -> string raises { A } uses { B }) -> void raises { A } uses { B }`, run), "EF104"},
		{"implementation widens failures", impl(`effect fn listen<A: raises, B: uses>(handler: effect fn(Req) -> string raises { A } uses { B }) -> void raises { A, Missing } uses { B }`, `        fail Missing`), "EF104"},
		{"implementation catches an abstract row", impl(good, `        let out = run handler(Req { path: "/x" }).catch<A>("fallback")
        void`), "EF125"},
		{"implementation runs the handler without raising its row", impl(`effect fn listen<A: raises, B: uses>(handler: effect fn(Req) -> string raises { A } uses { B }) -> void uses { B }`, run), "EF107"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := test.source
			if strings.Contains(source, "Missing") && !strings.Contains(source, "error Missing") {
				source = "error Missing\n" + source
			}
			r := Compile(source)
			if r.Checked || !contains(diagnosticCodes(r), test.want) {
				t.Fatalf("want %s, got checked=%v %+v", test.want, r.Checked, r.Diagnostics)
			}
		})
	}
	t.Run("renaming is accepted", func(t *testing.T) {
		if r := Compile(impl(good, run)); !r.Checked {
			t.Fatalf("conformance up to renaming refused: %+v", r.Diagnostics)
		}
	})
}

// B1: a provider-method body is checked against canonical row identities, the
// method's declared row plus the constructor's concrete captures. Each source
// below was admitted by the checker at 0a024e2 and failed at run time with a
// missing provider (f, col); f3 was sound only because the operation row
// carried R. Positive controls follow.
const rowBodyPrelude = `service C {
    effect fn ping() -> string
}
impl CP for C {
    effect fn ping() -> string {
        "pong"
    }
}
service D {
    effect fn ping() -> void
}
`

func TestProviderBodyRowsAreCanonical(t *testing.T) {
	for _, test := range []struct {
		name   string
		source string
	}{
		{"row omitted from the result", `service S {
    effect fn go<R: uses>(h: effect fn() -> void uses { R }) -> void
}
impl F for S {
    effect fn go<B: uses>(h: effect fn() -> void uses { B }) -> void {
        run h()
    }
}
effect fn cb() -> void uses { Console } {
    run Console.log("x")
}
effect fn main() -> void {
    run S.go(cb).provide<S>(F)
}
`},
		{"row omitted in the implementation only", `service S {
    effect fn go<R: uses>(h: effect fn() -> void uses { R }) -> void uses { R }
}
impl F for S {
    effect fn go<B: uses>(h: effect fn() -> void uses { B }) -> void {
        run h()
    }
}
effect fn main() -> void {
    void
}
`},
		{"capture spelled like a binder", rowBodyPrelude + `service S {
    effect fn go<R: uses>(h: effect fn() -> void uses { R }) -> void
}
impl F for S uses { C } {
    effect fn go<C: uses>(h: effect fn() -> void uses { C }) -> void {
        run h()
    }
}
effect fn cb() -> void uses { D } {
    run D.ping()
}
effect fn main() -> void {
    let f = run F().provide<C>(CP)
    run S.go(cb).provide<S>(f)
}
`},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, target := range []string{"go", "js"} {
				r := CompileFor(test.source, target)
				if r.Checked || !contains(diagnosticCodes(r), "EF108") {
					t.Fatalf("%s: want EF108, got checked=%v %+v", target, r.Checked, r.Diagnostics)
				}
			}
		})
	}
	for _, test := range []struct {
		name   string
		source string
	}{
		{"unused callback with an empty result row", `service S {
    effect fn go<R: uses>(h: effect fn() -> void uses { R }) -> void
}
impl F for S {
    effect fn go<B: uses>(h: effect fn() -> void uses { B }) -> void {
        void
    }
}
effect fn cb() -> void uses { Console } {
    run Console.log("x")
}
effect fn main() -> void {
    run S.go(cb).provide<S>(F)
}
`},
		{"concrete capture sharing a binder's spelling", rowBodyPrelude + positiveCaptureBody},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, target := range []string{"go", "js"} {
				if r := CompileFor(test.source, target); !r.Checked {
					t.Fatalf("%s: %+v", target, r.Diagnostics)
				}
			}
		})
	}
	providerConfigurationRowsAreCanonical(t)
}

func providerConfigurationRowsAreCanonical(t *testing.T) {
	t.Helper()
	tests := []struct {
		name, source, want string
	}{
		{"configuration service collides with a row binder", `service C {
    effect fn ping() -> void
}
service S {
    effect fn go<R: uses>(h: effect fn() -> void uses { R }) -> void uses { R }
}
impl F(config: effect fn() -> void uses { C }) for S {
    effect fn go<C: uses>(h: effect fn() -> void uses { C }) -> void uses { C } {
        run config()
    }
}
effect fn main() -> void {
    void
}
`, "EF108"},
		{"configuration service with a renamed row binder", `service C {
    effect fn ping() -> void
}
service S {
    effect fn go<R: uses>(h: effect fn() -> void uses { R }) -> void uses { R }
}
impl F(config: effect fn() -> void uses { C }) for S {
    effect fn go<B: uses>(h: effect fn() -> void uses { B }) -> void uses { B } {
        run config()
    }
}
effect fn main() -> void {
    void
}
`, "EF108"},
		{"configuration failure collides with a row binder", `error A
service S {
    effect fn go<E: raises>(h: effect fn() -> void raises { E }) -> void raises { E }
}
impl F(config: effect fn() -> void raises { A }) for S {
    effect fn go<A: raises>(h: effect fn() -> void raises { A }) -> void raises { A } {
        run config()
    }
}
effect fn main() -> void {
    void
}
`, "EF107"},
		{"configuration failure with a renamed row binder", `error A
service S {
    effect fn go<E: raises>(h: effect fn() -> void raises { E }) -> void raises { E }
}
impl F(config: effect fn() -> void raises { A }) for S {
    effect fn go<B: raises>(h: effect fn() -> void raises { B }) -> void raises { B } {
        run config()
    }
}
effect fn main() -> void {
    void
}
`, "EF107"},
		{"constructor rejects an abstract callback with a same-spelled row", `service C {
    effect fn ping() -> void
}
impl CP for C {
    effect fn ping() -> void { void }
}
service D {
    effect fn ping() -> void
}
service X {
    effect fn invoke() -> void
}
impl Inner(config: effect fn() -> void uses { C }) for X uses { C } {
    effect fn invoke() -> void { run config() }
}
service S {
    effect fn go<R: uses>(h: effect fn() -> void uses { R }) -> void
}
impl Outer for S uses { C } {
    effect fn go<C: uses>(h: effect fn() -> void uses { C }) -> void {
        let inner = run Inner(h)
        run X.invoke().provide<X>(inner)
    }
}
effect fn cb() -> void uses { D } { run D.ping() }
effect fn main() -> void {
    let outer = run Outer().provide<C>(CP)
    run S.go(cb).provide<S>(outer)
}
`, "EF106"},
		{"captured concrete Console with a same-spelled row binder", `service S {
    effect fn go<R: uses>(h: effect fn() -> void uses { R }) -> void
}
impl F(config: effect fn() -> void uses { Console }) for S uses { Console } {
    effect fn go<Console: uses>(h: effect fn() -> void uses { Console }) -> void {
        run config()
    }
}
effect fn configured() -> void uses { Console } {
    run Console.log("x")
}
effect fn noop() -> void {
    void
}
effect fn main() -> void {
    let f = run F(configured).provide<Console>(Stdout)
    run S.go(noop).provide<S>(f)
}
`, "accept"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, target := range []string{"go", "js"} {
				t.Run(target, func(t *testing.T) {
					r := CompileFor(test.source, target)
					if test.want == "accept" {
						if !r.Checked {
							t.Fatalf("expected acceptance, got %+v", r.Diagnostics)
						}
						return
					}
					if r.Checked || !contains(diagnosticCodes(r), test.want) {
						t.Fatalf("want %s, got checked=%v %+v", test.want, r.Checked, r.Diagnostics)
					}
				})
			}
		})
	}
}

// The method runs its concrete capture C and the callback's abstract row,
// which is also spelled C; both are declared, so both are admitted.
const positiveCaptureBody = `service S {
    effect fn go<R: uses>(h: effect fn() -> void uses { R }) -> void uses { R }
}
impl F for S uses { C } {
    effect fn go<C: uses>(h: effect fn() -> void uses { C }) -> void uses { C } {
        let p = run C.ping()
        run h()
    }
}
effect fn cb() -> void uses { Console } {
    run Console.log("x")
}
effect fn main() -> void {
    let f = run F().provide<C>(CP)
    run S.go(cb).provide<S>(f).provide<Console>(Stdout)
}
`
