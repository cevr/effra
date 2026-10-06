# Effra

Effra describes server programs using explicit, inspectable effect contracts.

## Language

**Record**: A nominal data type with declared fields. Construction checks every required field and its type.

**Closed enum**: A nominal set of alternatives, each with its own declared payload.

**Exhaustive match**: An interpretation that covers each declared alternative exactly once and executes only its selected arm.

**Failure payload**: The declared data carried by a named failure, separate from an ordinary success value.

**Effect contract**: The success value, named failures, and required services of a deferred Effra program.

**Failure row**: The unordered set of nominal failures admitted by an effect contract.

**Requirement row**: The unordered set of nominal services needed to execute an effect.

**Target provider**: An implementation of a service on a particular execution target.

**Provider construction contract**: The configuration and required services used to create a provider value, distinct from the contract of invoking its service operations.

**Provider recipe**: A deferred constructor with explicit configuration and construction requirements. Each execution materializes a provider value.

**Materialized provider value**: An already constructed service implementation that can be explicitly reused without reexecuting its recipe.

**Captured provider**: A provider value bound to the service values supplied during its construction. Its operations belong to the caller's current owning scope.

**Ownership provenance**: The relationship between a retained value and the lifetime that owns it. Borrowed, newly owned and unknown relationships carry different evidence.

**Semantic revision**: The identity of the checked snapshot described by inspection or diagnostics, including imported declaration data and behavior contracts when present.

**Managed fiber**: An execution of an Effra effect with an owner and a completion result. Its cancellation request and completed shutdown are distinct states.

**Owning scope**: The lifetime that owns managed fibers and resource releases. Its closure establishes their completed shutdown and cleanup.

**Host declaration**: A Go or TypeScript declaration supplying the native shape and identity of an imported value or callable.

**Codec** (specified, not yet implemented): A checked witness relating a wire type and a domain type through separately contracted decoding and encoding operations.

**Structural derivation** (specified): Generating a codec's structural rules from canonical checked data declarations and an explicit representation policy.

**Codec transformation** (specified): A checked conversion in a codec's decoding or encoding direction, carrying its own expected failures and required services. The two directions need not be mathematical inverses.

**Machine** (specified): A checked definition of state and event types, transition policies and state-owned behavior.

**Actor** (specified): A running instance of a machine, with its own current state, owning scope and event mailbox.

**State entry** (specified): One owned lifetime of an actor's current state, identified separately from that state's tag and payload.

**Re-entry** (specified): Closing a state entry and starting a fresh one, including when the state tag stays the same.

**Entry epoch** (specified): An identity distinguishing a state entry from prior entries so obsolete work cannot update a later entry.

**Binding contract**: Supplemental behavioral facts attached to a host declaration, including its cancellation, failure, resource, and trust policy.

**Request scope**: A fresh owning scope for one HTTP request, linked to connection and server cancellation. Completed shutdown includes its handler cleanup.

## Tooling and testing

**Lint advice**: Optional guidance about an admitted program, distinct from diagnostics that determine whether its contracts are valid.

**Expression anchor**: A source location identifying the expression described by a diagnostic or local type query.

**Dependency graph**: A revision-scoped view of program contracts and the relationships between functions, services and providers. Incoming relationships identify dependents.

**Owning test case**: A checked effect whose normal completion includes shutdown of its children and release of its resources.

**Live test mode**: Explicit permission for a test program to use host capabilities or real program time, distinct from fixture substitution.

**Test watchdog**: A harness deadline independent of program time. Forced termination leaves managed cleanup unconfirmed.

**Program time**: The time authority under which managed sleeps and deadlines execute. Test program time can advance independently of the watchdog's wall clock.
