# Native callable source corpus

Proposed finite Unit 2 syntax, retained before implementation. These examples are acceptance inputs, not a claim that the base compiler admits them. Ordinary calls do not supply row arguments. Library authors name finite row parameters using existing `raises` and `uses` vocabulary.

The service-composition pattern preserves callback execution rows:

```rust
error Missing
service Directory { effect fn get(key: string) -> string raises {Missing} }
effect fn lookup(key: string) -> string raises {Missing} uses {Directory} {
    run Directory.get(key)
}
effect fn dispatch<E: raises, R: uses>(
    operation: effect fn(string) -> string raises {E} uses {R}, key: string
) -> string raises {E} uses {R} {
    let pending = operation(key)
    run pending
}
effect fn request() -> string raises {Missing} uses {Directory} {
    run dispatch(lookup, "42")
}
```

The unrelated state-transition pattern uses the same function-value mechanism:

```rust
record State { name: string }
fn renamed(state: State, event: string) -> State { State { name: event } }
fn advance(step: fn(State, string) -> State, state: State, event: string) -> State {
    step(state, event)
}
effect fn main() -> string {
    let next = advance(renamed, State { name: "old" }, "new")
    next.name
}
```

Closed callback contracts use `fn(T) -> U` or `effect fn(T) -> U raises {Failures} uses {Services}`. Parentheses delimit a callable return type when an enclosing effect function also has its own rows. Omitted rows are empty. Pure and effectful callable kinds stay distinct. Parameters are contravariant, results covariant, and callback failure/service rows are subsets of the expected rows.

Finite row parameters are argument-driven. Several callbacks sharing a row parameter union their concrete rows. Unsupported ambiguous constraints diagnose instead of widening. Concrete catch/provide removes its label; subtracting a label from an unresolved abstract row requires an honest unsupported diagnostic until represented soundly. General type parameters, closure capture, generic data, interfaces and codecs remain outside this unit.

Ownership controls pair a callback returning its borrowed File input with one acquiring a new File under the invocation owner. They pass through multiple forwarding helpers, declaration orders and conditional callback choices. Unresolved callback results retain a deferred relation or conservative potential ownership; they cannot become harmless unknown. Scoped helpers may reject unresolved results conservatively. Record/provider placements must preserve complete contracts or diagnose explicitly.

Verification uses Compile/TypeAt/Find, actual Go and JS execution, and actual CLI/MCP inspection. Existing unsupported-source controls remain independent from admitted-source public projection refusal. No runtime factory/invoke optimization or performance claim belongs to this unit.

The row slice admits up to eight explicitly named `raises`/`uses` parameters on module functions. Each inferred callback row contains at most one variable; repeated uses collect the least union of actual argument rows. Distinct helpers may rename their row parameters while forwarding callbacks. Variables must occur in a direct callback argument row. Module function application infers them; first-class polymorphic functions and abstract row subtraction diagnose `EF125`. Closed recovery and provision retain their existing behavior. Canonical row tables expose qualified variable labels with their name, kind, declaring function and span, and application facts expose the inferred row bindings. Public projection limits charge these fields before materialization. Go erases row parameters from executable function signatures; JS declarations retain named generic error and service types.

The runnable `.ef` fixtures are [service composition](../../examples/callables-service.ef) and [state transition placement](../../examples/callables-state.ef). Both are checked and executed on Go and JS. JS declaration consumers infer independent witnesses at direct callback rows and use the common inferred union in nested callable positions; they cannot bypass nested contravariance through explicit witnesses.

Ownership evidence retains at most eight resolved named callees and 4,096 interned callback-result relations, with a substitution depth of 32. Aliases, conditional/match joins and function-returning helpers preserve supported evidence. A closing scope inside a generic helper, unknown record/foreign callback results, and exhausted evidence diagnose conservatively. Child and timeout ownership is preserved independently from the caller's invocation owner. This unit adds no anonymous functions, capture checking or recipe-typed parameter syntax.

The bundled HTTP serve operation declares a `typed-failure-response` callback policy. Its checked application exposes the absorbed handler failures and preserves every handler service requirement. `Handler` remains an explicit empty-row alias; ordinary helpers and record fields cannot erase wider rows by using that alias. The transport policy is specific to the declared operation and is reflected in JS consumer declarations.
