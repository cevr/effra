# Effra

Effra describes server programs using explicit, inspectable effect contracts.

## Language

**Record**: A nominal data type with declared fields. Construction checks every required field and its type.

**Closed enum**: A nominal set of alternatives, each with its own declared payload.

**Absence**: An explicitly selected closed alternative describing a missing value, distinct from an uninitialized field or a bare nil/null value.

**Void**: The no-value type, spelled `void`, describing successful completion without an information-bearing result. Its explicit expression is `void`. It is distinct from absence and from a computation that does not complete successfully.

**Option** (specified): A generic closed enum with `None` and `Some(T)` alternatives. `Some` contains an admitted value of `T`, never an unchecked nil host reference.

**Exhaustive match**: An interpretation that covers each declared alternative exactly once and executes only its selected arm.

**Failure payload**: The declared data carried by a named failure, separate from an ordinary success value.

**Effect contract**: The success value, named failures, and required services of a deferred Effra program.

**Failure row**: The unordered set of nominal failures admitted by an effect contract.

**Requirement row**: The unordered set of nominal services needed to execute an effect.

**Callable value**: A named function carried as a value with explicit parameter, result, failure and requirement contracts. Pure functions and functions constructing deferred effects have distinct callable kinds.

**Row parameter**: A declaration-qualified variable representing a finite failure or requirement row. Ordinary function application obtains its least bound from direct callback argument rows.

**Callback-result relation**: Retained evidence relating a callback invocation's returned handles to its resolved named callee and input ownership. It is separate from ownership of the callable value; an unresolved relation remains potential ownership.

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

**Native Go protocol** (specified): An imported Go interface and its method set, interpreted by Go's assignment rules. Its behavioral obligations remain distinct from type compatibility.

**Host borrow** (specified): Use of a native value under an existing owner's lifetime without acquiring release authority; retention and aliasing constraints remain explicit.

**Host adoption** (specified): Establishing managed release authority for an admitted native resource and its tracked aliases under an owning scope.

**Native descriptor** (specified): An OS-specific handle whose validity follows a native resource lifetime; it is distinct from the resource object and its interfaces.

**Codec** (specified, not yet implemented): A checked witness relating a wire type and a domain type through separately contracted decoding and encoding operations.

**Structural derivation** (specified): Generating a codec's structural rules from canonical checked data declarations and an explicit representation policy.

**Codec transformation** (specified): A checked conversion in a codec's decoding or encoding direction, carrying its own expected failures and required services. The two directions need not be mathematical inverses.

**Machine** (specified): A checked definition of state and event types, transition policies and state-owned behavior.

**Actor** (specified): An owned, addressable instance of behavior with typed messages/replies and explicit admission/concurrency policy. Ordinary handlers or receive loops can supply behavior; an effect without a message interface remains a fiber.

**Machine-backed actor** (specified): An actor whose behavior follows a checked machine plan, adding transitions and state-entry lifetimes to ordinary actor ownership and messaging.

**Supervisor** (proposed): An owner that observes completed child exits and applies an explicit restart, stop or escalation policy using fresh child factories and bounded restart attempts.

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

**Expression anchor**: A source location identifying the expression described by a diagnostic or local type query.

**Dependency graph**: A revision-scoped view of program contracts and the relationships between functions, services and providers. Incoming relationships identify dependents.

**Layer construction path** (specified): A source-grounded sequence of acquisition dependencies explaining a selected node, remaining input or startup failure, separate from service-operation call edges.

**Owning test case**: A checked effect whose normal completion includes shutdown of its children and release of its resources.

**Live test mode**: Explicit permission for a test program to use host capabilities or real program time, distinct from fixture substitution.

**Test watchdog**: A harness deadline independent of program time. Forced termination leaves managed cleanup unconfirmed.

**Program time**: The time authority under which managed sleeps and deadlines execute. Test program time can advance independently of the watchdog's wall clock.
