# Dependency-capturing provider construction

Implementation detail of [production foundations](production-foundations.md), not a separate architectural decision resolution.

Providers that need configuration or services must expose construction as an effect. A constructor captures the supplied dependency service values at execution, while its methods retain the invocation's fiber, cancellation and owner. A stored constructor recipe remains lazy: each `run` materializes a distinct provider value. A materialized provider value can be explicitly reused by multiple consumers; declaration names alone are not sharing keys.

Suggested source shape (the implementation may settle minor syntax while preserving these contracts):

```rust
service Names { effect fn get(id: string) -> string }
service Greeting { effect fn hello(id: string) -> string }

impl Prefixed(prefix: string) for Greeting uses {Names} {
    effect fn hello(id: string) -> string uses {Names} {
        let name = run Names.get(id)
        prefix + name
    }
}

effect fn main() -> void {
    let greeting = run Prefixed("Hello, ").provide<Names>(FixtureNames)
    let text = run Greeting.hello("42").provide<Greeting>(greeting)
    run Console.log(text).provide<Console>(Stdout)
}
```

The service operation's public contract requires `Greeting`; the constructor's contract requires `Names`. Provider method bodies may use only their explicit captured construction requirements, in addition to normal parameters. Missing dependencies diagnose the construction path. Self-contained existing named providers stay compatible.

A decisive case constructs with Names A, then invokes in a context supplied with Names B. The method still sees A. A second case invokes from a child scope and proves method work belongs to that child; captured construction FiberContext/Scope is forbidden. Native method wrappers must overlay dependency pointers while retaining current runtime state. JS wrappers must provide only the captured service keys, never replay an entire Effect Context containing runtime Scope/owner.

Graphs show constructor contracts, captured provider dependencies and values supplied at boundaries. Explicit reused values are one value; two constructor calls remain distinct. This stage does not claim a general memoized acquisition graph, fallible initializer language, provider cycle solving or lifecycle-safe escape of arbitrary captures. Those need a dedicated construction/acquisition contract and ownership evidence.

## Prior art: Elixir Plug

A module plug splits construction from invocation: `init/1` prepares the options that `call/2` receives. `init/1` "may be called during compilation and as such it must not return pids, ports or values that are specific to the runtime" (`lib/plug.ex:L20-L27` at Plug `73404f85`), and `Plug.Builder` initializes options at compile time by default (`lib/plug/builder.ex:L36-L37`). Effra keeps the split but moves construction to execution time. A constructor is an effect executed at `run`, it captures dependency values then, and methods keep the invocation's owner, so nothing in a provider recipe runs during compilation. Pins are in [PRIOR_ARTS](../../PRIOR_ARTS.md#elixir-with-erlangotp).
