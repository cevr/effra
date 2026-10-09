# Effra

Effra describes server programs using explicit, inspectable effect contracts.

## Language

**Record**: A nominal data type with declared fields. Construction checks every required field and its type.

**Closed enum**: A nominal set of alternatives, each with its own declared payload.

**Absence**: An explicitly selected closed alternative describing a missing value, distinct from an uninitialized field or a bare nil/null value.

**Void**: The no-value type, spelled `void`, describing successful completion without an information-bearing result. Its explicit expression is `void`. It is distinct from absence and from a computation that does not complete successfully.

**Option** (specified): A generic closed enum with `None` and `Some(T)` alternatives. `Some` contains an admitted value of `T`, never an unchecked nil host reference.

**Exhaustive match**: An interpretation that covers each declared alternative exactly once and executes only its selected arm.

**Signed-64 wrap** (specified): The portable `i64` arithmetic rule: each admitted `+`, `-`, `*`, `/` and unary `-` result is reduced to its two's-complement signed 64-bit residue, so `MIN / -1` is `MIN`. Division and remainder truncate toward zero.

**Divisor proof** (specified): Checked evidence that an `i64` divisor is not zero: a nonzero integer literal, or a local binding inside an `if` branch whose comparison of that binding with an integer literal excludes zero. `/` and `%` are admitted only with one, so division by zero is not representable in checked source.

**Failure payload**: The declared data carried by a named failure, separate from an ordinary success value.

**Effect contract**: The success value, named failures, and required services of a deferred Effra program.

**Semantic equivalence baseline** (acceptance term): A matched Effra, explicit
Go and TypeScript/Effect program pair that performs the same validation and
observable work while preserving the same failure/service rows, ownership,
cancellation and completed-cleanup behavior. The native Go member is an
optimized idiomatic Go control with the same contract. The JS member may use
the pinned Effect userland runtime or a generated/specialized implementation
behind the same default ABI. The baseline makes an abstraction's generated and
runtime costs attributable rather than inferred from unequal examples.

**Zero-cost abstraction obligation** (destination, not current support): A
candidate abstraction must make the illegal states and invalid operations
covered by its checked contract unrepresentable in checked Effra source, while
foreign and trusted behavior remains explicitly qualified, and then erase or
directly lower without an abstraction-induced executable size/runtime penalty
against a semantic equivalence baseline. Retained allocation, dispatch, module,
byte and residual-check costs must be visible in matched receipts. Native output
targets parity with or better performance than the optimized idiomatic Go
member. JS work must pursue every material measured lowering or specialization
opportunity, including match dispatch and generated effect-runtime paths, while
preserving the pinned default ABI and userland runtime contract. This obligation
never removes a required guardrail and does not promise universally zero
overhead or a fixed speedup multiplier.

**Failure row**: The unordered set of nominal failures admitted by an effect contract.

**Requirement row**: The unordered set of nominal services needed to execute an effect.

**Callable value**: A named function carried as a value with explicit parameter, result, failure and requirement contracts. Pure functions and functions constructing deferred effects have distinct callable kinds.

**Row parameter**: A declaration-qualified variable representing a finite failure or requirement row. Ordinary function application obtains its least bound from direct callback argument rows.

**Callback-result relation**: Retained evidence relating a callback invocation's returned handles to its resolved named callee and input ownership. It is separate from ownership of the callable value; an unresolved relation remains potential ownership.

**Construct admission**: The rule that a language construct, meaning syntax or a checker rule, exists only to make bad code unrepresentable where a library cannot.

**Notation exception**: A syntax form admitted without an unrepresentability argument because it desugars one-to-one, at parse time, into ordinary calls and adds no checking, typing, evaluation order or runtime behavior.

**Compiler-known construct**: A form whose meaning the compiler must know for checking, inference, plan facts, inspection or special lowering. By owner direction (2026-10-09) it is spelled as syntax or a predeclared language identifier.

**Predeclared language identifier**: A name the language specification defines in every module without an import or library declaration, such as `Fiber` or `Timeout`, following Go's predeclared `len` and `error`. Capability services are not predeclared language identifiers, even when the compiler currently declares them.

**Postfix construct**: A compiler-known construct written after a receiver with `.`, such as `.catch<E>(h)`, `.provide<S>(p)` or `.timeout(ms)`. The parser claims its word after any receiver. Declaring that word as a member name is proposed to become a diagnostic, but it is not one yet.

**Special-cased library item** (rejected pattern): A bundled, prelude or library declaration that the compiler recognises by identity or name to produce a semantic fact about it (a diagnostic, checked type, row or ownership fact), also called a lang item. An item stops being one only by losing every such branch or by becoming syntax. Backend retention or emission keyed on the item does not by itself make it one.

**Library intrinsic**: A target implementation the compiler substitutes for an ordinary declared library signature. The signature remains the only source of diagnostics, checked types, rows and ownership. The intrinsic may add backend facts such as retained runtime modules. The implementation of a syntax construct is part of that construct, not a library intrinsic.

**Language protocol**: A nominal row label whose charging or discharge a construct's own checking rule defines, such as `Foreign` for calls through `import go`. It is predeclared. An ordinary capability service, whose label enters a row only through declared operations or requirements, is not a language protocol.

**Pipe** (implemented): The notation `x |> f(args)`, which is the ordinary call `f(x, args)`.

**Receiver method** (admitted, not implemented): A function declared with an explicit receiver in its type's owner module, such as `fn (u: User) display()`. The call `u.display()` is exactly `User.display(u)`.

**Constant parameter default** (admitted, not implemented): A literal or named constant declared for a parameter and inserted at the call site when the argument is omitted.

**Visibility** (admitted, not implemented): Whether another module may name a declaration. Declarations are private by default and exported with `pub`; the case of a name carries no meaning.

**Construction authority** (admitted, not implemented): The right to construct values of a nominal type. A `pub readonly` type lets other modules read and match its values while only its owner module constructs them.

**Law** (admitted, not implemented): A named obligation declared in a service or contract and checked by a law suite. A refuted law refuses the build.

**JSX pragma** (admitted, not implemented): A user-defined selection of the factory, fragment and result types to which JSX notation desugars.

**Target provider**: An implementation of a service on a particular execution target.

**Provider construction contract**: The configuration and required services used to create a provider value, distinct from the contract of invoking its service operations.

**Provider recipe**: A deferred constructor with explicit configuration and construction requirements. Each execution materializes a provider value.

**Materialized provider value**: An already constructed service implementation that can be explicitly reused without reexecuting its recipe.

**Captured provider**: A provider value bound to the service values supplied during its construction. Its operations belong to the caller's current owning scope.

**Layer** (specified): A lazy checked acquisition plan selecting implementations and composing their construction dependencies. Its contract records exposed services, construction failures and unsatisfied inputs.

**Layer binding** (specified): A nominal service selection at a declaration site, distinct from its implementation's type and from an acquired provider instance.

**Shared acquisition node** (specified): One identified construction selection reused within a graph build under compatible input identities; concurrent consumers await the same acquisition.

**Graph build** (specified): One scope-owned execution of a layer plan, with its own acquisition table and completed startup or rollback outcome. Separate builds are independent.

**Layer replacement** (specified): A checked pre-acquisition substitution of one binding throughout a composed graph, followed by reconstruction of its effective construction edges and contract.

**Fresh layer build** (specified): An explicitly separate provision/build boundary with independent acquisition. A flat graph does not admit a second binding of one nominal service by copying its subtree.

**Ownership provenance**: The relationship between a retained value and the lifetime that owns it. Borrowed, newly owned and unknown relationships carry different evidence.

**Semantic revision**: The identity of the checked snapshot described by inspection or diagnostics, including imported declaration data and behavior contracts when present.

**Managed fiber**: An execution of an Effra effect with an owner and a completion result. Its cancellation request and completed shutdown are distinct states.

**Owning scope**: The lifetime that owns managed fibers and resource releases. Its closure establishes their completed shutdown and cleanup.

**Host declaration**: A Go or TypeScript declaration supplying the native shape and identity of an imported value or callable.

**Initialization root**: A declared foreign Go import's package initialization, retained in a native application independently of reachable calls and lowered as a named or blank Go import. Module availability, export-data discovery and metadata lookup are not initialization roots.

**Native Go protocol** (specified): An imported Go interface and its method set, interpreted by Go's assignment rules. Its behavioral obligations remain distinct from type compatibility.

**Host borrow** (specified): Use of a native value under an existing owner's lifetime without acquiring release authority; retention and aliasing constraints remain explicit.

**Host adoption** (specified): Establishing managed release authority for an admitted native resource and its tracked aliases under an owning scope.

**Native descriptor** (specified): An OS-specific handle whose validity follows a native resource lifetime; it is distinct from the resource object and its interfaces.

**Codec** (implemented for structural JSON): A checked witness relating a wire type and a domain type through separately contracted decoding and encoding operations.

**Structural derivation** (implemented for the first JSON profile): Generating a codec's structural rules from canonical checked data declarations and an explicit representation policy.

**Codec plan** (implemented): The bounded, shared structural DAG one derivation checks for a domain type under a profile. Both targets' engines execute the same plan; an application retains it only through an executed direction.

**Codec transformation** (specified): A checked conversion in a codec's decoding or encoding direction, carrying its own expected failures and required services. The two directions need not be mathematical inverses.

**Machine** (specified): A checked definition of state and event types, transition policies and state-owned behavior.

**Machine plan** (specified): The finite, source-revisioned provider-independent facts lowered from a checked machine declaration, including nominal state/event/outcome/output identities, callable rows, transition edges, entry/work identity rules and inspection data.

**Machine provider** (specified): An ordinary typed library, function or service implementation that consumes a machine plan and owns its execution, admission, scopes, cleanup and runtime observation without changing the plan's identities or public rows.

**Provider conformance** (specified): Evidence that independently authored machine providers execute the same checked plans on the same target with unchanged declarations/compiler selection while preserving the declared observable contracts and recording their separate runtime obligations.

**Effect runtime provider** (direction): An implementation of the lawful runtime contract that executes Effra effects on a target. Pinned Effect is the default and reference JavaScript provider, and `runtime/effra` is the native Go provider.

**Lawful runtime contract** (specified research): The host operations and laws an effect runtime provider must satisfy, with each obligation's evidence classified as nominally checked, trusted decision, tested law, refuted or unresolved/unavailable.

**Actor** (specified): An owned, addressable instance of behavior with typed messages/replies and explicit admission/concurrency policy. Ordinary handlers or receive loops can supply behavior; an effect without a message interface remains a fiber.

**Machine-backed actor** (specified): An actor whose behavior follows a checked machine plan, adding transitions and state-entry lifetimes to ordinary actor ownership and messaging.

**Supervisor** (proposed): An owner that observes completed child exits and applies an explicit restart, stop or escalation policy using fresh child factories and an explicit restart budget: a finite restart intensity, or `unbounded` flagged by the `no-unbounded-restart` justifier rule.

**Actor address** (proposed): A typed identity used to route to an actor independently of its current running instance; an address alone promises neither persistence nor delivery.

**Actor incarnation** (proposed): One running instance associated with an address, distinguished from its predecessors and from its individual state entries.

**Durable entity** (proposed): An addressable actor whose provider contract preserves declared domain data and acknowledged progress across process failure, with explicit storage, codec and ownership policies.

**Workflow replay** (proposed): Reconstructing execution from versioned recorded steps and outcomes under an explicit replay contract; ordinary effect recipes do not imply replay safety.

**Machine step** (specified): An ordinary pure or effectful function that consumes a state and event and returns an explicit transition decision. Effectful evaluation has a machine-backed actor-owned scope and completes before state commit.

**State entry** (specified): One owned lifetime of a machine-backed actor's current state, identified separately from that state's tag and payload.

**Re-entry** (specified): Closing a state entry and starting a fresh one, including when the state tag stays the same.

**Entry epoch** (specified): An identity distinguishing a state entry from prior entries so obsolete work cannot update a later entry.

**Binding contract**: Supplemental behavioral facts attached to a host declaration, including its cancellation, failure, resource, and trust policy.

**Request scope**: A fresh owning scope for one HTTP request, linked to connection and server cancellation. Completed shutdown includes its handler cleanup.

## Tooling and testing

**Lint advice**: Optional guidance about an admitted program, distinct from diagnostics that determine whether its contracts are valid.

**Justifier rule**: A default-preset lint rule that flags a legal but reviewable choice, such as an `unbounded` budget, until a suppression records the reason.

**Suppression status**: The outcome of one reasoned lint suppression in one analysis. It is applied when it removed a finding of its rule, unused when that rule ran and found nothing to remove, or not evaluated, with a reason, when the rule did not run.

**Lint cost receipt**: Raw per-run wall-clock measurements of one lint analysis. Frontend checking, fact extraction, each rule pack's fact serialization and process phases, and the merge are kept separate. A receipt carries no aggregate and supports no performance claim on its own.

**Expression anchor**: A source location identifying the expression described by a diagnostic or local type query.

**Dependency graph**: A revision-scoped view of program contracts and the relationships between functions, services and providers. Incoming relationships identify dependents.

**Layer construction path** (specified): A source-grounded sequence of acquisition dependencies explaining a selected node, remaining input or startup failure, separate from service-operation call edges.

**Owning test case**: A checked effect whose normal completion includes shutdown of its children and release of its resources.

**Live test mode**: Explicit permission for a test program to use host capabilities or real program time, distinct from fixture substitution.

**Test watchdog**: A harness deadline independent of program time. Forced termination leaves managed cleanup unconfirmed.

**Application receipt** (implemented): A raw measurement of one built application: the runtime modules and declarations its plan retained, its generated files, imports, dependencies, executable or module bytes and symbols, and the external runtime it does not contain. It reports the compiler distribution separately and makes no performance claim.

**Program time**: The time authority under which managed sleeps and deadlines execute. Test program time can advance independently of the watchdog's wall clock.
